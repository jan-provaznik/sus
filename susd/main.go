// 2026 Jan Provaznik (jan@provaznik.pro)
//

package main

import "os"
import "fmt"
import "flag"
import "time"
import "github.com/jan-provaznik/sus"
import "github.com/NVIDIA/go-nvml/pkg/nvml"

var flagMonitorDelay time.Duration
var flagCurrentLimit float64 

func main () {
	os.Exit(work())
}

func work () (int) {
	flag.DurationVar(& flagMonitorDelay, "t", 2 * time.Second, 
		"Monitoring interval")
	flag.Float64Var(& flagCurrentLimit, "u", 8.5, 
		"Maximal current draw per wire (in amperes)")
	flag.Parse()

	if flagCurrentLimit < 0 {
		fmt.Println("Invalid currentLimit: must be > 0")
		return 1
	}
	if flagCurrentLimit > 10 {
		fmt.Println("Invalid currentLimit: must be < 10")
		return 1
	}

	if flagMonitorDelay > 5 * time.Second {
		fmt.Println("Warning! Setting the monitoring interval too large is discouraged")
	}
	if flagCurrentLimit > 9.2 {
		fmt.Println("Warning! Setting the current limit above 9.2 (in amperes) is strongly discouraged")
	}

	if ret := nvml.Init(); ret != nvml.SUCCESS {
		fmt.Println("nvmlInit failed (%w)", ret)
		return 1
	}
	defer nvml.Shutdown()

	list, err := sus.FindAstralDevices()
	if err != nil {
		fmt.Println(err)
		return 1
	}

	if len(list) < 1 {
		fmt.Println("Could not find any compatible devices")
		return 0
	}

	for index, device := range list {
		fmt.Printf("Detected device (%d) identified by (%s)\n",
			index, device.Identifier())
	}

	for {
		for index, device := range list {
			if err := deviceMonitor(index, device); err != nil {
				fmt.Println(err)
				return 1
			}
		}

		time.Sleep(flagMonitorDelay)
	}
}

func deviceMonitor (index int, device sus.AstralDevice) (error) {
	pins, err := device.QueryDevicePins()
	if err != nil {
		return err
	}

	maximum := 0.0
	for _, pin := range pins {
		if value := pin.Current(); value > maximum {
			maximum = value
		}
	}

	if maximum < flagCurrentLimit {
		return nil
	}

	fmt.Printf("Device (%d) identified by (%s)\n",
		index, device.Identifier())
	fmt.Printf("... detected overload %.1f A (limit %.1f A)\n",
		maximum, flagCurrentLimit)

	scale := flagCurrentLimit / maximum
	if scale > 1 {
		return nil
	}

	return device.ScaleDeviceLoad(scale)
}

