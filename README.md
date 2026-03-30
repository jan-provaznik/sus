# Introduction

  - The 12V-2x6 (12VHPWR) connectors are notorious for their flammability.
  - The ASUS ROG Astral and Matrix implementation of the RTX 5090 monitors voltage and current flowing through connector pins.

  This project provides a native Linux implementation of the current monitoring
  functionality of ASUS ROG Astral/Matrix devices.

# Supported Devices

  Support is currently limited to:
  
  - ROG Astral Black/White (ROG-ASTRAL-RTX5090-O32G)
  - ROG Matrix Platinum

# Usage

## SUSM

  The bin/susm binary implements a simple monitor that measures the following data.

  - Voltage
  - Wattage
  - Amperage

  The following are usable command flags.

  **Monitoring interval**
  
  Description: Change the refresh interval.
  Default: 1s (250ms, 1s, 2m)

  ```
  susm -t <duration>
  ```

  **No Clear**

  Description: Enable or Disable output scrolling.
  Default: False

  ```
  susm -no-clear
  ```

  **Log Output**

  Description: When enabled (specified) additionally writes output to file
  Default: Disabled

  ```
  susm -log <path>
  ```

  **Log Append**

  Description: Append to existing logfile
  Default: True

  ```
  susm -log-append=<true|false>
  ```


## SUSD

  The bin/susd binary implements a simple daemon that attempts to reduce the power draw when it detects either overload on any of the connectors pins or when there is a significant mismatch between individual wires.

## Examples

![alt text](https://github.com/jan-provaznik/sus/blob/mainster/pics/output.png?raw=true)

## Build Instructions

  1. Make sure you install go/go-toolchain.

  2. Then navigate to ./sus

  3. Run `Make`
    - all
    - clean
    - bin
    - bim/susm
    - bin/susd
