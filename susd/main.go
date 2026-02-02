// 2026 Jan Provaznik (jan@provaznik.pro)
//

package main

import "os"
import "fmt"
import "flag"
import "time"
import "github.com/jan-provaznik/sus"
import "github.com/NVIDIA/go-nvml/pkg/nvml"

var upperDrawLimit float64 = 9.2
var lowerDrawLimit float64 = 0.0
var matchDrawLimit float64 = 1.0

func main () {
	defer nvml.Shutdown()

	interval := flag.Duration("t", 2 * time.Second, "Monitoring interval")

	flag.Float64Var(& upperDrawLimit, "u", 8.5, 
		"Maximal power draw per wire (A). Going above 9.2 A is dangerous.")
	flag.Parse()

	if upperDrawLimit > 15 || upperDrawLimit < 0 {
		fmt.Println("Invalid upperDrawLimit. Restrict to 1 <= value < 15.")
		os.Exit(1)
	}

	if upperDrawLimit > 9.2 {
		fmt.Println("Warning! Setting the maximal power draw above the 9.2 A is strongly discouraged.")
	}

	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		fmt.Println("nvmlInit failed")
		os.Exit(1)
	}

	list, err := sus.FindAstralDevices()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if len(list) < 1 {
		fmt.Println("Could not find any compatible devices. Exiting.")
		os.Exit(0)
	}

	for index, device := range list {
		fmt.Printf("Detected device (%d) identified by (%s)\n",
			index, device.Identifier())
	}
	fmt.Println()

	for {
		for index, device := range list {
			err := deviceMonitor(index, device)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
		}
		time.Sleep(* interval)
	}
}

func deviceMonitor (index int, device sus.AstralDevice) error {
	// ... load, as reported via asus interface
	pins, err := sus.ReadAstralDevicePins(device)
	if err != nil {
		return err
	}

	maximum := 0.0
	for _, pin := range pins {
		if value := pin.Current(); value > maximum {
			maximum = value
		}
	}

	if maximum> upperDrawLimit {
		deviceLimit(index, device, maximum)
	}

	return nil
}

func deviceLimit (index int, device sus.AstralDevice, current float64) {
	fmt.Printf("Device (%d) identified by (%s)\n",
		index, device.Identifier())
	fmt.Printf("... detected overload %.1f A (limit %.1f A)\n",
		current, upperDrawLimit)

	rate := current / upperDrawLimit
	target := 600 * rate

	if target < 400 {
		sus.LimitAstralDeviceLoad(device, target)
	} else {
		sus.LimitAstralDeviceFreq(device)
	}
}

