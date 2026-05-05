package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/jan-provaznik/sus"
)

const (
	voltage     = 12.0
	balanceWarn = 0.88
)

type WaybarOutput struct {
	Text    string `json:"text"`
	Tooltip string `json:"tooltip"`
	Class   string `json:"class"`
}

type Config struct {
	CriticalColor     string
	WarningColor      string
	CriticalThreshold float64
	WarningThreshold  float64
	Amps              bool
}

type Metrics struct {
	FormattedPins []string
	Total         float64
	Unit          string
	BalanceRate   float64
}

func parseConfig() *Config {
	var cfg Config

	flag.StringVar(&cfg.CriticalColor, "c", "#ff5555", "HEX color for critical threshold")
	flag.StringVar(&cfg.WarningColor, "w", "#f1fa8c", "HEX color for warning threshold")
	flag.Float64Var(&cfg.CriticalThreshold, "tc", 108.0, "Critical wattage threshold")
	flag.Float64Var(&cfg.WarningThreshold, "tw", 96.0, "Warning wattage threshold")
	flag.BoolVar(&cfg.Amps, "a", false, "Show amperage instead of wattage")

	flag.Parse()

	return &cfg
}

func getColor(watts float64, cfg *Config) string {
	if watts >= cfg.CriticalThreshold {
		return cfg.CriticalColor
	}
	if watts >= cfg.WarningThreshold {
		return cfg.WarningColor
	}
	return ""
}

func initNVML() {
	if ret := nvml.Init(); ret != nvml.SUCCESS {
		printError("NVML Init Failed")
		os.Exit(1)
	}
}

func getDevice() sus.AstralDevice {
	list, err := sus.FindAstralDevices()
	if err != nil || len(list) < 1 {
		printError("GPU Not Found")
		os.Exit(0)
	}
	return list[0]
}

func getMetrics(device sus.AstralDevice, cfg *Config) Metrics {
	pins, err := sus.ReadAstralDevicePins(device)
	if err != nil {
		printError("I2C Read Failed")
		os.Exit(1)
	}

	var formatted []string
	total, min, max := 0.0, math.MaxFloat64, 0.0
	unit := "W"
	if cfg.Amps {
		unit = "A"
	}

	for _, pin := range pins {
		watts := pin.Drawing()
		amps := watts / voltage

		displayVal := watts
		if cfg.Amps {
			displayVal = amps
		}

		total += displayVal
		min = math.Min(min, amps)
		max = math.Max(max, amps)

		if color := getColor(watts, cfg); color != "" {
			formatted = append(formatted, fmt.Sprintf("<span color='%s'>%.1f%s</span>", color, displayVal, unit))
		} else {
			formatted = append(formatted, fmt.Sprintf("%.1f%s", displayVal, unit))
		}
	}

	balance := 1.0
	if max > 0 {
		balance = min / max
	}

	return Metrics{
		FormattedPins: formatted,
		Total:         total,
		Unit:          unit,
		BalanceRate:   balance,
	}
}

func printError(msg string) {
	output := WaybarOutput{
		Text: "ERROR!",
		Tooltip: msg,
		Class: "error",
	}
	res, _ := json.Marshal(output)
	fmt.Printf(string(res))
}

func printOutput(m Metrics, deviceID string) {
	output := WaybarOutput{
		Text:    fmt.Sprintf("󰾲 󱐋 %s", strings.Join(m.FormattedPins, " ")),
		Tooltip: fmt.Sprintf("Total Draw: %.1f%s\nBalance Rate: %.2f\nDevice: %s", m.Total, m.Unit, m.BalanceRate, deviceID),
		Class:   "normal",
	}

	if m.BalanceRate < balanceWarn {
		output.Class = "warning"
	}

	res, _ := json.Marshal(output)
	fmt.Println(string(res))
}

func main() {
	cfg := parseConfig()
	initNVML()
	defer nvml.Shutdown()

	device := getDevice()
	metrics := getMetrics(device, cfg)
	printOutput(metrics, device.Identifier())
}
