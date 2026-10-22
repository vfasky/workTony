package app

import (
	"os"
	"strconv"
)

func itoa(n int) string { return strconv.Itoa(n) }

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) }

func userHome() (string, error) { return os.UserHomeDir() }
