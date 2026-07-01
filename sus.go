// 2026 Jan Provaznik (jan@provaznik.pro).0
//

package sus

import "os"
import "fmt"
import "slices"
import "strings"
import "encoding/binary"

import "github.com/NVIDIA/go-nvml/pkg/nvml"
import "github.com/khirono/go-i2c/smbus"

// Constants
//

var nvidiaCompatibleDevice = []uint32 { 0x2b8510de }
var astralCompatibleDevice = []uint32 {
	0x89e31043, // ROG-ASTRAL-RTX5090-O32G
	0x8a2e1043, // ROG-ASTRAL-RTX5090-O32G-WHITE
}

// Exported struct: AstralDevicePin
//
// .Voltage () (float64)
// .Current () (float64)
// .Drawing () (float64)

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

// Exported struct: AstralDevice
//
// .Identifier      ()               (string)
// .QueryDeviceLoad ()               (uint32, error)
// .QueryDevicePins ()               (uint32, error)
// .ScaleDeviceLoad (factor float64) (error)

type AstralDevice struct {
	sensorNumber int
	deviceHandle nvml.Device
	deviceDetailPci nvml.PciInfo 
	deviceDetailIdentifier string

	// ... constraints (power management, clock frequencies)
	devicePowerConstraintLower uint32
	devicePowerConstraintUpper uint32
	deviceClockMinimumGraphics uint32
	deviceClockMinimumMemories uint32
}

// Returns a readable identification of the device
func (self AstralDevice) Identifier () string {
	return self.deviceDetailIdentifier
}

// Queries the current power draw of the device, returns value in mW
func (self AstralDevice) QueryDeviceLoad () (uint32, error) {
	value, ret := nvml.DeviceGetPowerUsage(self.deviceHandle)
	if ret != nvml.SUCCESS {
		return 0, fmt.Errorf("nvmlDeviceGetPowerUsage failed (%w)", ret)
	}
	return value, nil
}

// Scales the current power draw of the device by 0 < scale < 1
func (self AstralDevice) ScaleDeviceLoad (scale float64) (error) {
	if scale < 0 {
		return fmt.Errorf("Invalid scale: must be 0 < scale")
	}
	if scale > 1 {
		return fmt.Errorf("Invalid scale: must be scale < 1")
	}

	current, err := self.QueryDeviceLoad()
	if err != nil {
		return err
	}

	target := uint32(float64(current) * scale)

	// ... target below the configurable limit, declare emergency and throttle
	if target < self.devicePowerConstraintLower {
		return throttleDeviceClock(self)
	}

	return limitAstralDeviceLoad(self, target)
}

// Queries the current power draw of its pins, returns an array of readings
func (self AstralDevice) QueryDevicePins () ([]AstralDevicePin, error) {
	// Sensor address and register
	// ... via https://long-cat.net/gitea/moosecrap/evga-icx
	// ... via https://github.com/LibreHardwareMonitor/LibreHardwareMonitor
	// Sensor interaction (smbus)
	// ... via https://github.com/Timic3/astral-power-monitoring

	bus, err := smbus.Open(self.sensorNumber)
	if err != nil {
		return nil, err
	}
	defer bus.Close()

	if err := bus.SetSlaveAddr(0x2B, false); err != nil {
		return nil, err
	}

	buffer := make([]byte, 24)
	length, err := bus.ReadI2CBlockData(0x80, buffer)
	if err != nil {
		return nil, err
	}

	if length != 24 {
		return nil, fmt.Errorf("Could not read sensor device, content too short")
	}

	result := make([]AstralDevicePin, 6)
	for index := range 6 {
		start := 4 * index
		value := parseRegisterBuffer(buffer[start:start + 4])
		result[index] = value
	}

	return result, nil
}

// Exported functions (with backwards compatibility)
//

func FindAstralDevices () ([] AstralDevice, error) {
	return findAstralDevices()
}
func ReadAstralDevicePins (target AstralDevice) ([]AstralDevicePin, error) {
	return target.QueryDevicePins()
}
func ReadAstralDeviceLoad (target AstralDevice) (float64, error) {
	value, err := target.QueryDeviceLoad()
	if err != nil {
		return 0, err
	}
	return float64(value) / 1000.0, nil
}
func LimitAstralDevice (device AstralDevice, scale float64) (error) {
	return device.ScaleDeviceLoad(scale)
}

// Implementation details 
//


// Sets the clocks to their minimal values to prevent a catastrophic meltdown.
func throttleDeviceClock (self AstralDevice) (error) {
	var ret nvml.Return

	ret = nvml.DeviceSetGpuLockedClocks(self.deviceHandle, 0, self.deviceClockMinimumGraphics)
	if ret != nvml.SUCCESS {
		return fmt.Errorf("nvmlDeviceSetGpuLockedClocks failed (%w)", ret)
	}

	ret = nvml.DeviceSetMemoryLockedClocks(self.deviceHandle, 0, self.deviceClockMinimumMemories)
	if ret != nvml.SUCCESS {
		return fmt.Errorf("nvmlDeviceSetMemoryLockedClocks failed (%w)", ret)
	}

	return nil
}

// Sets the power limit using nvmlDeviceSetPowerManagementLimit procedure
func limitAstralDeviceLoad (self AstralDevice, target uint32) (error) {
	if target < self.devicePowerConstraintLower {
		return fmt.Errorf("target < devicePowerConstraintLower")
	}
	if target > self.devicePowerConstraintUpper {
		return fmt.Errorf("target > devicePowerConstraintUpper")
	}

	ret := nvml.DeviceSetPowerManagementLimit(self.deviceHandle, target)
	if ret != nvml.SUCCESS {
		return fmt.Errorf("nvmlDeviceSetPowerManagementLimit failed")
	}

	return nil
}


// Supporting functions
//

func parseRegisterBuffer (buffer []byte) AstralDevicePin {
	// Voltage (mV)
	wordOne := binary.BigEndian.Uint16(buffer[0:2])

	// Current (mA)
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
		return final, fmt.Errorf("Could not find sensor device")
	}

	return final, nil
}

func findAstralDevices () ([] AstralDevice, error) {
	var found [] AstralDevice

	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("nvmlDeviceGetCount failed (%w)", ret)
	}

	for index := range count {
		device, ret := nvml.DeviceGetHandleByIndex(index)
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("nvmlDeviceGetHandleByIndex failed (%w)", ret)
		}

		info, ret := nvml.DeviceGetPciInfo(device)
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("nvmlDeviceGetPciInfo failed (%w)", ret)
		}

		if ! slices.Contains(nvidiaCompatibleDevice, info.PciDeviceId) {
			continue
		}
		if ! slices.Contains(astralCompatibleDevice, info.PciSubSystemId) {
			continue
		}

		uuid, ret := nvml.DeviceGetUUID(device)
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("nvmlDeviceGetUUID failed (%w)", ret)
		}

		number, err := findAstralDeviceSensorNumber(info)
		if err != nil {
			return nil, err
		}

		limitLower, limitUpper, ret := nvml.DeviceGetPowerManagementLimitConstraints(device)
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("nvmlDeviceGetPowerManagementLimitConstraints failed (%w)", ret)
		}

		limitClockGraphics, limitClockMemories, err := readClockConstraints(device)
		if err != nil {
			return nil, err
		}

		current := AstralDevice {
			sensorNumber: number,
			deviceHandle: device,
			deviceDetailPci: info,
			deviceDetailIdentifier: uuid,
			devicePowerConstraintLower: limitLower,
			devicePowerConstraintUpper: limitUpper,
			deviceClockMinimumGraphics: limitClockGraphics,
			deviceClockMinimumMemories: limitClockMemories,
		}

		found = append(found, current)
	}

	return found, nil
}

func readClockConstraints (device nvml.Device) (uint32, uint32, error) {
	// Determine the lowest supported graphics and memory clock frequencies by
	// examining all the available performance states. 
	//
	// We can not use
	// ... nvmlDeviceGetSupportedMemoryClocks
	// ... nvmlDeviceGetSupportedGraphicsClocks
	// because go-nvml (v0.13.3) implements the C interface incorrectly.

	var leastFrequencyGraphics uint32 = 0xffffff
	var leastFrequencyMemories uint32 = 0xffffff

	// nvmlDeviceGetSupportedPerformanceStates
	var supported []nvml.Pstates 

	supported, ret := nvml.DeviceGetSupportedPerformanceStates(device)
	if ret != nvml.SUCCESS {
		return 0, 0, fmt.Errorf("nvmlDeviceGetSupportedPerformanceStates failed (%w)", ret)
	}

	for _, pstate := range supported {
		valueGraphics, valueMemories, err := readClockConstraintsOfPerformanceState(device, pstate)
		if err != nil {
			return 0, 0, err
		}

		if valueGraphics < leastFrequencyGraphics {
			leastFrequencyGraphics = valueGraphics
		}
		if valueMemories < leastFrequencyMemories {
			leastFrequencyMemories = valueMemories
		}
	}

	return leastFrequencyGraphics, leastFrequencyMemories, nil
}

func readClockConstraintsOfPerformanceState (device nvml.Device, pstate nvml.Pstates) (uint32, uint32, error) {
	var valueGraphics, valueMemories uint32

	valueGraphics, _, ret := nvml.DeviceGetMinMaxClockOfPState(device, nvml.CLOCK_GRAPHICS, pstate)
	if ret != nvml.SUCCESS {
		return 0, 0, fmt.Errorf("nvmlDeviceGetMinMaxClockOfPState (0) failed (%w)", ret)
	}

	valueMemories, _, ret = nvml.DeviceGetMinMaxClockOfPState(device, nvml.CLOCK_MEM, pstate)
	if ret != nvml.SUCCESS {
		return 0, 0, fmt.Errorf("nvmlDeviceGetMinMaxClockOfPState (2) failed (%w)", ret)
	}

	return valueGraphics, valueMemories, nil
}

