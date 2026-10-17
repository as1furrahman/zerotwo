package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

type winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

// GetTerminalRows queries TIOCGWINSZ on stdout (fd 1), falls back to stderr (fd 2), then 25 rows.
func GetTerminalRows() int {
	var ws winsize

	// 1. Check stdout
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(syscall.Stdout), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws))) // #nosec G103 -- ioctl TIOCGWINSZ query
	if err == 0 && ws.Row > 0 {
		return int(ws.Row)
	}

	// 2. Check stderr
	_, _, err = syscall.Syscall(syscall.SYS_IOCTL, uintptr(syscall.Stderr), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws))) // #nosec G103 -- ioctl TIOCGWINSZ query
	if err == 0 && ws.Row > 0 {
		return int(ws.Row)
	}

	return 25 // standard terminal fallback
}

// RenderPaged prints text with automatic screen-height pagination if attached to a TTY.
func RenderPaged(text string) {
	rows := GetTerminalRows()
	lines := strings.Split(text, "\n")

	// If text fits in one screen or stdout is not a terminal, print directly
	if len(lines) <= rows || !IsStdoutTerminal() {
		fmt.Println(text)
		return
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		// No TTY, print normally
		fmt.Println(text)
		return
	}
	defer tty.Close()

	reader := bufio.NewReader(tty)
	pageSize := rows - 2
	if pageSize < 5 {
		pageSize = 5
	}

	for i := 0; i < len(lines); i += pageSize {
		end := i + pageSize
		if end > len(lines) {
			end = len(lines)
		}
		for j := i; j < end; j++ {
			fmt.Println(lines[j])
		}

		if end < len(lines) {
			fmt.Fprintf(tty, "\033[7m-- More (%d%%) [Press Enter to continue, 'q' to stop] --\033[0m", (end*100)/len(lines))
			ans, _ := reader.ReadString('\n')
			fmt.Fprint(tty, "\r\033[K") // clear prompt line
			if strings.TrimSpace(ans) == "q" {
				break
			}
		}
	}
}

func IsStdoutTerminal() bool {
	var ws winsize
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(syscall.Stdout), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws))) // #nosec G103 -- ioctl TIOCGWINSZ query
	return err == 0
}

func IsStdinTerminal() bool {
	var ws winsize
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(syscall.Stdin), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws))) // #nosec G103 -- ioctl TIOCGWINSZ query
	return err == 0
}
