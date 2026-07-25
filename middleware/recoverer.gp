package middleware

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
)

func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			if recovered == http.ErrAbortHandler {
				panic(recovered)
			}
			if entry := GetLogEntry(r); entry != nil {
				entry.Panic(recovered, debug.Stack())
			} else {
				PrintPrettyStack(recovered)
			}
			if r.Header.Get("Connection") != "Upgrade" {
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

var recovererErrorWriter io.Writer = os.Stderr

func PrintPrettyStack(recovered any) {
	printPrettyStack(recovered, true)
}

func printPrettyStack(recovered any, useColor bool) {
	output, err := formatPrettyStack(debug.Stack(), recovered, useColor)
	if err != nil {
		_, _ = recovererErrorWriter.Write(debug.Stack())
		return
	}
	_, _ = recovererErrorWriter.Write(output)
}

func formatPrettyStack(stack []byte, recovered any, useColor bool) ([]byte, error) {
	buffer := &bytes.Buffer{}
	writeStackColor(buffer, false, "\033[31;1m", "\n")
	writeStackColor(buffer, useColor, "\033[36;1m", " panic: ")
	writeStackColor(buffer, useColor, "\033[34;1m", "%v", recovered)
	writeStackColor(buffer, false, "\033[37;1m", "\n \n")

	rawLines := strings.Split(string(stack), "\n")
	lines := make([]string, 0, len(rawLines))
	for index := len(rawLines) - 1; index > 0; index-- {
		lines = append(lines, rawLines[index])
		if strings.HasPrefix(rawLines[index], "panic(") {
			if len(lines) >= 2 {
				lines = lines[:len(lines)-2]
			}
			break
		}
	}
	for left := len(lines)/2 - 1; left >= 0; left-- {
		right := len(lines) - 1 - left
		lines[left], lines[right] = lines[right], lines[left]
	}
	for index, line := range lines {
		decorated, err := decorateStackLine(line, useColor, index)
		if err != nil {
			return nil, err
		}
		buffer.WriteString(decorated)
	}
	return buffer.Bytes(), nil
}

func decorateStackLine(line string, useColor bool, number int) (string, error) {
	line = strings.TrimSpace(line)
	if strings.Contains(line, ".go:") {
		return decorateSourceLine(line, useColor, number)
	}
	if strings.HasSuffix(line, ")") {
		return decorateCallLine(line, useColor, number)
	}
	return fmt.Sprintf("    %s\n", line), nil
}

func decorateCallLine(line string, useColor bool, number int) (string, error) {
	call := strings.LastIndexByte(line, '(')
	if call < 0 {
		return "", errors.New("not a function call line")
	}
	pkg := line[:call]
	method := ""
	if slash := strings.LastIndex(pkg, string(os.PathSeparator)); slash < 0 {
		if dot := strings.IndexByte(pkg, '.'); dot > 0 {
			method, pkg = pkg[dot:], pkg[:dot]
		}
	} else {
		method, pkg = pkg[slash+1:], pkg[:slash+1]
		if dot := strings.IndexByte(method, '.'); dot > 0 {
			pkg += method[:dot]
			method = method[dot:]
		}
	}
	buffer := &bytes.Buffer{}
	pkgColor, methodColor := "\033[33m", "\033[32;1m"
	if number == 0 {
		writeStackColor(buffer, useColor, "\033[31;1m", " -> ")
		pkgColor, methodColor = "\033[35;1m", "\033[31;1m"
	} else {
		writeStackColor(buffer, useColor, "\033[37;1m", "    ")
	}
	writeStackColor(buffer, useColor, pkgColor, "%s", pkg)
	writeStackColor(buffer, useColor, methodColor, "%s\n", method)
	return buffer.String(), nil
}

func decorateSourceLine(line string, useColor bool, number int) (string, error) {
	goFile := strings.LastIndex(line, ".go:")
	if goFile < 0 {
		return "", errors.New("not a source line")
	}
	sourcePath, lineNumber := line[:goFile+3], line[goFile+3:]
	slash := strings.LastIndex(sourcePath, string(os.PathSeparator))
	if slash < 0 {
		return "", errors.New("source path has no directory")
	}
	directory, file := sourcePath[:slash+1], sourcePath[slash+1:]
	if space := strings.IndexByte(lineNumber, ' '); space > 0 {
		lineNumber = lineNumber[:space]
	}
	buffer := &bytes.Buffer{}
	fileColor, numberColor := "\033[36;1m", "\033[32;1m"
	if number == 1 {
		writeStackColor(buffer, useColor, "\033[31;1m", " ->   ")
		fileColor, numberColor = "\033[31;1m", "\033[35;1m"
	} else {
		writeStackColor(buffer, false, "\033[37;1m", "      ")
	}
	writeStackColor(buffer, useColor, "\033[37;1m", "%s", directory)
	writeStackColor(buffer, useColor, fileColor, "%s", file)
	writeStackColor(buffer, useColor, numberColor, "%s", lineNumber)
	if number == 1 {
		writeStackColor(buffer, false, "\033[37;1m", "\n")
	}
	writeStackColor(buffer, false, "\033[37;1m", "\n")
	return buffer.String(), nil
}

func writeStackColor(writer io.Writer, useColor bool, color, format string, values ...any) {
	if IsTTY && useColor {
		_, _ = io.WriteString(writer, color)
	}
	_, _ = fmt.Fprintf(writer, format, values...)
	if IsTTY && useColor {
		_, _ = io.WriteString(writer, "\033[0m")
	}
}
