package watcher

import "strconv"

func parseInt(s string) (int, error) {
	return strconv.Atoi(s)
}

func fmtInt(v int) string {
	return strconv.Itoa(v)
}
