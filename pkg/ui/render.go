package ui

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
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

// GetTerminalRows queries $LINES, TIOCGWINSZ on stdout (fd 1), stderr (fd 2), /dev/tty, then 25 rows.
func GetTerminalRows() int {
	// 1. Check environment variable LINES (allows explicit override in scripts/kterm)
	if linesStr := os.Getenv("LINES"); linesStr != "" {
		if r, err := strconv.Atoi(linesStr); err == nil && r > 0 {
			return r
		}
	}

	var ws winsize

	// 2. Check stdout
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(syscall.Stdout), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws))) // #nosec G103 -- ioctl TIOCGWINSZ query
	if err == 0 && ws.Row > 0 {
		return int(ws.Row)
	}

	// 3. Check stderr
	_, _, err = syscall.Syscall(syscall.SYS_IOCTL, uintptr(syscall.Stderr), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws))) // #nosec G103 -- ioctl TIOCGWINSZ query
	if err == 0 && ws.Row > 0 {
		return int(ws.Row)
	}

	// 4. Check /dev/tty
	if tty, err := os.Open("/dev/tty"); err == nil {
		defer tty.Close()
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, tty.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws))) // #nosec G103
		if errno == 0 && ws.Row > 0 {
			return int(ws.Row)
		}
	}

	return 25 // standard terminal fallback
}

// GetTerminalCols queries $COLUMNS, TIOCGWINSZ on stdout (fd 1), stderr (fd 2), /dev/tty, then 80 columns.
func GetTerminalCols() int {
	// 1. Check environment variable COLUMNS (allows explicit override in scripts/kterm)
	if colsStr := os.Getenv("COLUMNS"); colsStr != "" {
		if c, err := strconv.Atoi(colsStr); err == nil && c > 0 {
			return c
		}
	}

	var ws winsize

	// 2. Check stdout
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(syscall.Stdout), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws))) // #nosec G103 -- ioctl TIOCGWINSZ query
	if err == 0 && ws.Col > 0 {
		return int(ws.Col)
	}

	// 3. Check stderr
	_, _, err = syscall.Syscall(syscall.SYS_IOCTL, uintptr(syscall.Stderr), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws))) // #nosec G103 -- ioctl TIOCGWINSZ query
	if err == 0 && ws.Col > 0 {
		return int(ws.Col)
	}

	// 4. Check /dev/tty
	if tty, err := os.Open("/dev/tty"); err == nil {
		defer tty.Close()
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, tty.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws))) // #nosec G103
		if errno == 0 && ws.Col > 0 {
			return int(ws.Col)
		}
	}

	return 80 // standard terminal fallback
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
			cols := GetTerminalCols()
			prompt := fmt.Sprintf("-- More (%d%%) [Press Enter to continue, 'q' to stop] --", (end*100)/len(lines))
			if cols < 60 {
				prompt = fmt.Sprintf("-- More (%d%%) [Enter / q] --", (end*100)/len(lines))
			}
			fmt.Fprintf(tty, "\033[7m%s\033[0m", prompt)
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
