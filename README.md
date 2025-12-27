# Project Diva Controller

Use your tablet/phone as a touch controller for Project Diva on Linux.

https://github.com/user-attachments/assets/ccafbbb6-cff3-453e-975a-8ef371ecd940

## Build

```bash
go build .
```
## Run

```bash
sudo ./diva-controller
```

Then open `http://YOUR_IP:3939` on your tablet.

## Options

```
-port          Server port (default 3939)
-triangle      Key for △ (default W)
-square        Key for □ (default A)
-cross         Key for ✕ (default S)
-circle        Key for ○ (default D)
-verbose       Print touch events
```
