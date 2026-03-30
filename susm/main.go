// 2026 Jan Provaznik (jan@provaznik.pro)
//

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/jan-provaznik/sus"
)

func clearScreen() {
	// Clear screen + move cursor to home
	fmt.Print("\033[2J\033[H")
}

func hideCursor() { fmt.Print("\033[?25l") }
func showCursor() { fmt.Print("\033[?25h") }

func ensureDirForFile(path string) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == "/" {
		return nil
	}
	return os.MkdirAll(dir, 0755)
}

func writeLog(path string, appendMode bool, s string) error {
	if path == "" {
		return nil
	}
	if err := ensureDirForFile(path); err != nil {
		return err
	}

	flags := os.O_CREATE | os.O_WRONLY
	if appendMode {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	f, err := os.OpenFile(path, flags, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(s)
	return err
}

func main() {
	interval := flag.Duration("t", time.Second, "Monitoring interval")
	noClear := flag.Bool("no-clear", false, "Do not clear screen each refresh")

	// Logging
	logPath := flag.String("log", "", "Write refresh snapshots to this file")
	logAppend := flag.Bool("log-append", true, "Append to log file (if false, overwrite each refresh)")

	flag.Parse()

	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		fmt.Println("nvmlInit failed:", ret)
		os.Exit(1)
	}
	defer nvml.Shutdown()

	// Handle Ctrl-C / SIGTERM so we restore cursor properly
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigc
		showCursor()
		fmt.Print("\n")
		os.Exit(0)
	}()

	hideCursor()
	defer showCursor()

	list, err := sus.FindAstralDevices()
	if err != nil {
		fmt.Println(err)
		return
	}
	if len(list) < 1 {
		fmt.Println("Could not find any compatible devices. Exiting.")
		return
	}

	for {
		var frame bytes.Buffer

		// Build one frame for both screen + log
		frame.WriteString(fmt.Sprintf("susm — refresh %s  (Ctrl-C to quit)\n", interval.String()))
		frame.WriteString(fmt.Sprintf("timestamp: %s\n\n", time.Now().Format(time.RFC3339)))

		for index, device := range list {
			if err := deviceReportTo(&frame, index, device); err != nil {
				// don't os.Exit — allow defers (cursor restore) to run
				fmt.Println(err)
				return
			}
		}

		// Screen output
		if !*noClear {
			clearScreen()
		}
		fmt.Print(frame.String())

		// Log output
		if *logPath != "" {
			logChunk := frame.String() + "\n---\n\n"
			if err := writeLog(*logPath, *logAppend, logChunk); err != nil {
				// Don't kill monitoring just because logging failed
				fmt.Fprintf(os.Stderr, "log write failed: %v\n", err)
			}
		}

		time.Sleep(*interval)
	}
}

func deviceReportTo(w *bytes.Buffer, index int, device sus.AstralDevice) error {
	// ... load, as reported via nvml
	load, err := sus.ReadAstralDeviceLoad(device)
	if err != nil {
		return err
	}

	// ... pins, as reported via asus interface
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
		totalDraw += value
	}

	matchDraw := 0.0
	if upperDraw > 0 {
		matchDraw = lowerDraw / upperDraw
	}

	// ... report header
	fmt.Fprintf(w, "Device (%d) known as: (%s)\n", index, device.Identifier())
	fmt.Fprintf(w, "... Total load %5.1f W\n", load)
	fmt.Fprintf(w, "... Total draw %5.1f W (min %5.1f max %5.1f W) rate %.2f\n",
		totalDraw, lowerDraw, upperDraw, matchDraw)

	// ... per-pin detail with amps
	fmt.Fprintln(w, "... Pins (V / A / W):")
	fmt.Fprintf(w, "    %2s  %7s  %7s  %7s\n", "#", "V", "A", "W")

	totalA := 0.0
	for i, pin := range pins {
		v := pin.Voltage()
		a := pin.Current()
		pw := pin.Drawing()
		totalA += a
		fmt.Fprintf(w, "    %2d  %7.3f  %7.3f  %7.1f\n", i, v, a, pw)
	}
	fmt.Fprintf(w, "    %2s  %7s  %7.3f  %7s\n", "", "Total:", totalA, "")
	fmt.Fprintln(w)

	return nil
}
