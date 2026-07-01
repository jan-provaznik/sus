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

func main () {
	os.Exit(work())
}

func work () (int) {
	flag.DurationVar(& flagMonitorDelay, "t", 2 * time.Second, 
		"Monitoring interval")
	flag.Parse()

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
		fmt.Println("Could not find any compatible devices.")
		return 0
	}

	for {
		for index, device := range list {
			if err := deviceReport(index, device); err != nil {
				fmt.Println(err)
				return 1
			}
		}
		fmt.Println()

		time.Sleep(flagMonitorDelay)
	}
}

func deviceReport (index int, device sus.AstralDevice) (error) {
	load, err := device.QueryDeviceLoad()
	if err != nil {
		return err
	}

	pins, err := device.QueryDevicePins()
	if err != nil {
		return err
	}

	totalDraw := 0.0
	upperDraw := 0.0
	lowerDraw := 1e6

	for _, pin := range pins {
		value := pin.Drawing()
		if value > upperDraw {
			upperDraw = value
		}
		if value < lowerDraw {
			lowerDraw = value
		}
		totalDraw = totalDraw + value
	}

	mismatch := lowerDraw / upperDraw

	// ... report
	fmt.Printf("Device (%d) known as (%s)\n", 
		index, device.Identifier())
	fmt.Printf("... total load %5.1f W\n", float64(load) / 1000.0)
	fmt.Printf("... total draw %5.1f W (min %5.1f max %5.1f W) mismatch %.2f\n", 
		totalDraw, lowerDraw, upperDraw, mismatch)

	fmt.Printf("... pins  draw ")
	for _, pin := range pins {
		value := pin.Drawing()
		fmt.Printf("%5.1f W ", value)
	}
	fmt.Println()

	return nil
}

