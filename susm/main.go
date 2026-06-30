// 2026 Jan Provaznik (jan@provaznik.pro)
//

package main

import "os"
import "fmt"
import "flag"
import "time"
import "github.com/jan-provaznik/sus"
import "github.com/NVIDIA/go-nvml/pkg/nvml"

func main () {
	os.Exit(work())
}

func work () int {
	interval := flag.Duration("t", time.Second, "Monitoring interval")
	flag.Parse()

	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		fmt.Println("nvmlInit failed")
		return 1
	}
	defer nvml.Shutdown()

	list, err := sus.FindAstralDevices()
	if err != nil {
		fmt.Println(err)
		return 1
	}

	if len(list) < 1 {
		fmt.Println("Could not find any compatible devices. Exiting gracefully.")
		return 0
	}

	for {
		for index, device := range list {
			err := deviceReport(index, device)
			if err != nil {
				fmt.Println(err)
				return 2
			}
		}
		fmt.Println()
		time.Sleep(* interval)
	}
}

func deviceReport (index int, device sus.AstralDevice) error {
	// ... load, as reported via nvml
	load, err := sus.ReadAstralDeviceLoad(device)
	if err != nil {
		return err
	}

	// ... load, as reported via asus interface
	pins, err := sus.ReadAstralDevicePins(device)
	if err != nil {
		return err
	}

	// ... calculate statistics
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

	// ... calculate draw match
	matchDraw := lowerDraw / upperDraw

	// ... report
	fmt.Printf("Device (%d) known as (%s)\n", 
		index, device.Identifier())
	fmt.Printf("... total load %5.1f W\n", load)
	fmt.Printf("... total draw %5.1f W (min %5.1f max %5.1f W) rate %.2f\n", 
		totalDraw, lowerDraw, upperDraw, matchDraw)

	fmt.Printf("... pins  draw ")
	for _, pin := range pins {
		value := pin.Drawing()
		fmt.Printf("%5.1f W ", value)
	}
	fmt.Println()

	return nil
}

