// 2026 Jan Provaznik (jan@provaznik.pro)
//

package sus

import "os"
import "fmt"
import "cmp"
import "slices"
import "strings"
import "encoding/binary"

import "github.com/NVIDIA/go-nvml/pkg/nvml"
import "github.com/khirono/go-i2c/smbus"

// Constants
//

var nvidiaCompatibleDevice = []uint32 { 0x2b8510de }
var astralCompatibleDevice = []uint32 { 0x89e31043 }

// Exported types and methods
//

type AstralDevice struct {
	sensorNumber int
	deviceHandle nvml.Device
	deviceDetailPci nvml.PciInfo 
	deviceDetailIdentifier string
}

func (self AstralDevice) Identifier () string {
	return self.deviceDetailIdentifier
}

type AstralDevicePin struct {
	voltage float64
	current float64 
}

func (self AstralDevicePin) Voltage () float64 {
	return self.voltage
}

func (self AstralDevicePin) Current () float64 {
	return self.current
}

func (self AstralDevicePin) Drawing () float64 {
	return self.voltage * self.current
}

// Exported functions
//

func FindAstralDevices () ([]AstralDevice, error) {
	var found []AstralDevice

	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("nvmlDeviceGetCount failed")
	}

	for index := range count {
		device, ret := nvml.DeviceGetHandleByIndex(index)
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("nvmlDeviceGetHandleByIndex failed")
		}

		info, ret := nvml.DeviceGetPciInfo(device)
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("nvmlDeviceGetPciInfo failed")
		}

		if ! slices.Contains(nvidiaCompatibleDevice, info.PciDeviceId) {
			continue
		}
		if ! slices.Contains(astralCompatibleDevice, info.PciSubSystemId) {
			continue
		}

		uuid, ret := nvml.DeviceGetUUID(device)
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("nvmlDeviceGetUUID failed")
		}

		number, err := findAstralDeviceSensorNumber(info)
		if err != nil {
			return nil, err
		}

		current := AstralDevice {
			sensorNumber: number,
			deviceHandle: device,
			deviceDetailPci: info,
			deviceDetailIdentifier: uuid,
		}

		found = append(found, current)
	}

	return found, nil
}

func ReadAstralDevicePins (target AstralDevice) ([]AstralDevicePin, error) {
	// Sensor address and register
	// ... via https://long-cat.net/gitea/moosecrap/evga-icx
	// ... via https://github.com/LibreHardwareMonitor/LibreHardwareMonitor
	//
	// Sensor interaction (smbus)
	// ... via https://github.com/Timic3/astral-power-monitoring

	bus, err := smbus.Open(target.sensorNumber)
	if err != nil {
		return nil, err
	}

	err = bus.SetSlaveAddr(0x2B, false)
	if err != nil {
		return nil, err
	}

	buffer := make([]byte, 24)
	length, err := bus.ReadI2CBlockData(0x80, buffer)
	if err != nil {
		return nil, err
	}

	if length != 24 {
		return nil, fmt.Errorf("could not read sensor device")
	}

	result := make([]AstralDevicePin, 6)
	for index := range 6 {
		start := 4 * index
		result[index] = readBuffer(buffer[start:start + 4])
	}

	return result, nil
}

func ReadAstralDeviceLoad (target AstralDevice) (float64, error) {
	// nvmlDeviceGetPowerUsage f
	// ... deals in mW

	value, ret := nvml.DeviceGetPowerUsage(target.deviceHandle)
	if ret != nvml.SUCCESS {
		return -1, fmt.Errorf("nvmlDeviceGetPowerUsage failed")
	}
	return float64(value) / 1000, nil
}

// Emergency actions
//

func LimitAstralDeviceFreq (target AstralDevice) (uint32, error) {
	current, ret := nvml.DeviceGetClockInfo(target.deviceHandle, nvml.CLOCK_GRAPHICS)
	if ret != nvml.SUCCESS {
		return 0, fmt.Errorf("nvmlDeviceGetClockInfo failed")
	}

	value :=  int32(current)
	limit := uint32(clamp(value - 500, 100, value))

	ret = nvml.DeviceSetGpuLockedClocks(target.deviceHandle, 0, limit)
	if ret != nvml.SUCCESS {
		return 0, fmt.Errorf("nvmlDeviceSetGpuLockedClocks failed")
	}

	return limit, nil
}

func LimitAstralDeviceLoad (target AstralDevice, limitValue float64) (float64, error) {
	var watts float64

	// nvmlDeviceGetPowerManagementLimitConstraints
	// nvmlDeviceSetPowerManagementLimit
	// ... deal in mW

	limitLower, limitUpper, ret := nvml.DeviceGetPowerManagementLimitConstraints(target.deviceHandle)
	if ret != nvml.SUCCESS {
		return watts, fmt.Errorf("nvmlDeviceGetPowerManagementLimitConstraints failed")
	}
	
	// ... power limit can be only set within the (lower, upper) range
	limit := clamp(uint32(limitValue), limitLower, limitUpper)

	// ... 
	ret = nvml.DeviceSetPowerManagementLimit(target.deviceHandle, limit)
	if ret != nvml.SUCCESS {
		return watts, fmt.Errorf("nvmlDeviceSetPowerManagementLimit failed")
	}

	watts = float64(limit) / 1000
	return watts, nil
}

// Supporting functions
//

func readBuffer (buffer []byte) AstralDevicePin {
	wordOne := binary.BigEndian.Uint16(buffer[0:2])
	wordTwo := binary.BigEndian.Uint16(buffer[2:4])

	return AstralDevicePin {
		voltage: float64(wordOne) / 1000, 
		current: float64(wordTwo) / 1000,
	}
}

func findAstralDeviceSensorNumber (info nvml.PciInfo) (int, error) {
	root := fmt.Sprintf("/sys/bus/pci/devices/%04x:%02x:%02x.0",
		info.Domain, info.Bus, info.Device)

	final := 0xffff
	value := 0xffff

	entries, err := os.ReadDir(root)
	if err != nil {
		return 0xffff, err
	}

	for _, item := range entries {
		if ! strings.HasPrefix(item.Name(), "i2c-") {
			continue
		}

		num, err := fmt.Sscanf(item.Name(), "i2c-%d", & value)
		if err != nil {
			return 0xffff, err
		}
		if num != 1 {
			continue
		}
		if value < final {
			final = value
		}
	}

	if final == 0xffff {
		return final, fmt.Errorf("could not find sensor device")
	}

	return final, nil
}

func clamp[V cmp.Ordered] (value V, lower V, upper V) V {
	if value > upper {
		return upper
	}
	if value < lower {
		return lower
	}
	return value
}

