// 2026 Jan Provaznik (jan@provaznik.pro)
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

// Exported type: AstralDevicePin
//

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

// Exported type: AstralDevice
//

type AstralDevice struct {
	sensorNumber int
	deviceHandle nvml.Device
	deviceDetailPci nvml.PciInfo 
	deviceDetailIdentifier string

	// ... constraints
	devicePowerConstraintLower uint32
	devicePowerConstraintUpper uint32
	deviceClockMinimumGraphics uint32
	deviceClockMinimumMemories uint32
}

func (self AstralDevice) Identifier () string {
	return self.deviceDetailIdentifier
}

func (self AstralDevice) ReadDeviceLoad () (uint32, error) {
	// nvmlDeviceGetPowerUsage (mW)
	value, ret := nvml.DeviceGetPowerUsage(self.deviceHandle)
	if ret != nvml.SUCCESS {
		return 0, fmt.Errorf("nvmlDeviceGetPowerUsage failed")
	}
	return value, nil
}

func (self AstralDevice) LimitDevice (factor float64) (error) {
	if factor > 1 {
		return fmt.Errorf("Invalid factor: must be factor < 1")
	}
	if factor < 0 {
		return fmt.Errorf("Invalid factor: must be 0 < factor")
	}

	load, err := self.ReadDeviceLoad()
	if err != nil {
		return err
	}

	target := uint32(float64(load) * factor)
	if target < self.devicePowerConstraintLower {
		return limitAstralDeviceClock(self)
	}

	return limitAstralDeviceLoad(self, target)
}

func (self AstralDevice) ReadDevicePins () ([]AstralDevicePin, error) {
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
		return nil, fmt.Errorf("could not read sensor device")
	}

	result := make([]AstralDevicePin, 6)
	for index := range 6 {
		start := 4 * index
		value := parseRegisterBuffer(buffer[start:start + 4])
		result[index] = value
	}

	return result, nil
}

// Exported functions
//

func FindAstralDevices () ([] AstralDevice, error) {
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

		current, err := makeAstralDevice(device, info)
		if err != nil {
			return nil, err
		}

		found = append(found, * current)
	}

	return found, nil
}

func ReadAstralDevicePins (target AstralDevice) ([]AstralDevicePin, error) {
	return target.ReadDevicePins()
}
func ReadAstralDeviceLoad (target AstralDevice) (uint32, error) {
	return target.ReadDeviceLoad()
}
func LimitAstralDevice (device AstralDevice, factor float64) (error) {
	return device.LimitDevice(factor)
}

// Sets the clocks to their minimal values to prevent a catastrophic meltdown.
//

func limitAstralDeviceClock (target AstralDevice) (error) {
	var ret nvml.Return

	ret = nvml.DeviceSetGpuLockedClocks(target.deviceHandle, 0, target.deviceClockMinimumGraphics)
	if ret != nvml.SUCCESS {
		return fmt.Errorf("nvmlDeviceSetGpuLockedClocks failed")
	}

	ret = nvml.DeviceSetMemoryLockedClocks(target.deviceHandle, 0, target.deviceClockMinimumMemories)
	if ret != nvml.SUCCESS {
		return fmt.Errorf("nvmlDeviceSetMemoryLockedClocks failed")
	}

	return nil
}

// Uses the native nvmlDeviceSetPowerManagementLimit facilities
// to control the maximal power draw of target device.
//
// Note: limitValue is specified in mW

func limitAstralDeviceLoad (target AstralDevice, limitValue uint32) (error) {
	if limitValue < target.devicePowerConstraintLower {
		return fmt.Errorf("limitValue < devicePowerConstraintLower")
	}
	if limitValue > target.devicePowerConstraintUpper {
		return fmt.Errorf("limitValue > devicePowerConstraintUpper")
	}

	// ... nvmlDeviceSetPowerManagementLimit (mW)
	ret := nvml.DeviceSetPowerManagementLimit(target.deviceHandle, limitValue)
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
		return final, fmt.Errorf("could not find sensor device")
	}

	return final, nil
}

func makeAstralDevice (device nvml.Device, pcinfo nvml.PciInfo) (* AstralDevice, error) {
	uuid, ret := nvml.DeviceGetUUID(device)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("nvmlDeviceGetUUID failed")
	}

	number, err := findAstralDeviceSensorNumber(pcinfo)
	if err != nil {
		return nil, err
	}

	// nvmlDeviceGetPowerManagementLimitConstraints (mW)
	limitLower, limitUpper, ret := nvml.DeviceGetPowerManagementLimitConstraints(device)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("nvmlDeviceGetPowerManagementLimitConstraints failed")
	}

	// nvmlDeviceGetSupportedPerformanceStates
	// nvmlDeviceGetMinMaxClockOfPState
	limitClockGraphics, limitClockMemories, err := readClockConstraints(device)
	if err != nil {
		return nil, err
	}
	
	return & AstralDevice {
		sensorNumber: number,
		deviceHandle: device,
		deviceDetailPci: pcinfo,
		deviceDetailIdentifier: uuid,
		devicePowerConstraintLower: limitLower,
		devicePowerConstraintUpper: limitUpper,
		deviceClockMinimumGraphics: limitClockGraphics,
		deviceClockMinimumMemories: limitClockMemories,
	}, nil
}

func readClockConstraints (device nvml.Device) (uint32, uint32, error) {
	// Determine the lowest supported graphics and memory clock frequencies by
	// examining all the available performance states. 
	//
	// We can not use
	// ... nvmlDeviceGetSupportedMemoryClocks
	// ... nvmlDeviceGetSupportedGraphicsClocks
	// because go-nvml (v0.13.3) implements the C interface incorrectly.

	var leastFrequencyGraphics uint32 = 0xffff
	var leastFrequencyMemories uint32 = 0xffff

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

func readClockConstraintsOfPerformanceState (device nvml.Device, pstate nvml.Pstates) (uint32, uint32 , error) {
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

