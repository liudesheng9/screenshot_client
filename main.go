package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var Global_sig_run int = 1
var Global_sig_lock sync.Mutex
var Global_sig_current_run int
var Global_sig_current_run_lock sync.Mutex
var Global_writer_end int = 0

type Task func(args ...interface{}) (interface{}, error)

type WorkerStatus struct {
	WorkerID    int
	WorkerLabel string
	Count       int
	Filename    string
	Stage       string
	Elapsed     string
}

type ProgressV2 struct {
	Total          int
	Target         int
	WorkerStatuses []WorkerStatus
}

var ansiOutputSupport struct {
	once    sync.Once
	enabled bool
}

func retry_task(task Task, print_err bool, args ...interface{}) interface{} {
	for {
		result, err := task(args...)
		if err == nil {
			return result
		} else {
			if print_err {
				fmt.Printf("Error: %v\n", err)
			}
			time.Sleep(5 * time.Second)
		}
	}
}

func title_print() {
	fmt.Println(` ____    ____  ____   _____  _____  _   _  ____   _   _   ___  _____ `)
	fmt.Println(`/ ___|  / ___||  _ \ | ____|| ____|| \ | |/ ___| | | | | / _ \|_   _|`)
	fmt.Println(`\___ \ | |    | |_) ||  _|  |  _|  |  \| |\___ \ | |_| || | | | | |  `)
	fmt.Println(` ___) || |___ |  _ < | |___ | |___ | |\  | ___) ||  _  || |_| | | |  `)
	fmt.Println(`|____/  \____||_| \_\|_____||_____||_| \_||____/ |_| |_| \___/  |_|  `)
	fmt.Println("CLIENT")
}

func readLine(reader *bufio.Reader) (string, error) {
	s, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

func dialWithRetry(address string, attempts int, delay time.Duration) (net.Conn, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		conn, err := net.DialTimeout("tcp", address, 5*time.Second)
		if err == nil {
			enableNoDelay(conn)
			return conn, nil
		}
		lastErr = err
		time.Sleep(delay)
	}
	return nil, lastErr
}

func enableNoDelay(conn net.Conn) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}
	_ = tcpConn.SetNoDelay(true)
}

func sendCommandAndWait(conn net.Conn, cmd string) (string, error) {
	_, err := conn.Write([]byte(cmd))
	if err != nil {
		return "", err
	}
	for {
		var buf [4096]byte
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := conn.Read(buf[:])
		if err != nil {
			if opErr, ok := err.(net.Error); ok && opErr.Timeout() {
				continue
			}
			return "", err
		}
		return strings.TrimSpace(string(buf[:n])), nil
	}
}

func sendCommand(conn net.Conn, cmd string) error {
	_, err := conn.Write([]byte(cmd))
	return err
}

func formatProgressDisplayLine(line string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 2 || fields[0] != "PROGRESS" {
		return "", false
	}

	workers := make([]string, 0, len(fields)-1)
	total := ""
	for _, field := range fields[1:] {
		if strings.HasPrefix(field, "W") && strings.Contains(field, ":") {
			workers = append(workers, field)
			continue
		}
		if strings.HasPrefix(field, "T:") {
			total = strings.TrimPrefix(field, "T:")
		}
	}

	if len(workers) == 0 && total == "" {
		return "", false
	}
	if total == "" {
		total = "0"
	}
	return strings.TrimSpace(strings.Join(workers, " ") + " Total:" + total), true
}

func parseProgressV2(line string) (ProgressV2, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 2 || fields[0] != "PROGRESS_V2" {
		return ProgressV2{}, false
	}

	totals := strings.TrimPrefix(fields[1], "T:")
	parts := strings.SplitN(totals, "/", 2)
	if len(parts) != 2 {
		return ProgressV2{}, false
	}
	total, err := strconv.Atoi(parts[0])
	if err != nil {
		return ProgressV2{}, false
	}
	target, err := strconv.Atoi(parts[1])
	if err != nil {
		return ProgressV2{}, false
	}

	statuses := make([]WorkerStatus, 0, len(fields)-2)
	for _, workerField := range fields[2:] {
		workerParts := strings.SplitN(workerField, ":", 5)
		if len(workerParts) != 5 {
			return ProgressV2{}, false
		}
		workerLabel, workerID, ok := parseWorkerLabel(workerParts[0])
		if !ok {
			return ProgressV2{}, false
		}
		count, err := strconv.Atoi(workerParts[1])
		if err != nil {
			return ProgressV2{}, false
		}

		statuses = append(statuses, WorkerStatus{
			WorkerID:    workerID,
			WorkerLabel: workerLabel,
			Count:       count,
			Filename:    workerParts[2],
			Stage:       workerParts[3],
			Elapsed:     workerParts[4],
		})
	}

	return ProgressV2{
		Total:          total,
		Target:         target,
		WorkerStatuses: statuses,
	}, true
}

func parseWorkerLabel(label string) (string, int, bool) {
	workerLabel := strings.TrimSpace(label)
	if workerLabel == "" {
		return "", 0, false
	}

	if strings.HasPrefix(workerLabel, "W") {
		workerID, err := strconv.Atoi(strings.TrimPrefix(workerLabel, "W"))
		if err != nil {
			return "", 0, false
		}
		return fmt.Sprintf("W%d", workerID), workerID, true
	}

	workerSep := strings.LastIndex(workerLabel, "-W")
	if workerSep <= 0 || workerSep >= len(workerLabel)-2 {
		return "", 0, false
	}
	workerID, err := strconv.Atoi(workerLabel[workerSep+2:])
	if err != nil {
		return "", 0, false
	}
	return workerLabel, workerID, true
}

func formatWorkerTable(progress ProgressV2) string {
	if len(progress.WorkerStatuses) == 0 {
		return fmt.Sprintf("Progress %d/%d", progress.Total, progress.Target)
	}

	const maxFileLength = 30
	header := fmt.Sprintf("Progress %d/%d", progress.Total, progress.Target)
	columns := fmt.Sprintf("%-8s %-30s %-8s %s", "Worker", "File", "Stage", "Time")
	rows := make([]string, 0, len(progress.WorkerStatuses)+2)
	rows = append(rows, header, columns)
	for _, status := range progress.WorkerStatuses {
		workerLabel := status.WorkerLabel
		if workerLabel == "" {
			workerLabel = fmt.Sprintf("W%d", status.WorkerID)
		}
		filename := status.Filename
		stage := status.Stage
		elapsed := status.Elapsed

		if filename == "" || filename == "-" || stage == "idle" {
			filename = "-"
		}
		if stage == "" {
			stage = "idle"
		}
		if elapsed == "" {
			elapsed = "-"
		}

		filename = truncateFilenameFromLeft(filename, maxFileLength)
		rows = append(rows, fmt.Sprintf("%-8s %-30s %-8s %s", workerLabel, filename, stage, elapsed))
	}
	return strings.Join(rows, "\n")
}

func truncateFilenameFromLeft(filename string, maxLength int) string {
	if maxLength < 4 {
		maxLength = 4
	}
	runes := []rune(filename)
	if len(runes) <= maxLength {
		return filename
	}
	return "..." + string(runes[len(runes)-(maxLength-3):])
}

func formatDoneDisplayLine(line string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 2 || fields[0] != "DONE" {
		return "", false
	}

	values := make(map[string]string)
	for _, field := range fields[1:] {
		kv := strings.SplitN(field, "=", 2)
		if len(kv) != 2 {
			continue
		}
		values[kv[0]] = kv[1]
	}

	orderedKeys := []string{"copied", "exist", "failed", "skipped", "dest"}
	out := make([]string, 0, len(orderedKeys))
	for _, key := range orderedKeys {
		if value, ok := values[key]; ok {
			out = append(out, key+"="+value)
		}
	}
	if len(out) == 0 {
		return "", false
	}
	return "DONE " + strings.Join(out, " "), true
}

func streamCopyResponse(conn net.Conn) error {
	connReader := bufio.NewReader(conn)
	timeoutCount := 0
	lastProgressWidth := 0
	progressActive := false
	lastTableLines := 0
	defer flushProgressLine(&progressActive, &lastProgressWidth)
	defer flushProgressTable(&lastTableLines)

	for {
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		line, err := connReader.ReadString('\n')
		if err != nil {
			if opErr, ok := err.(net.Error); ok && opErr.Timeout() {
				timeoutCount++
				if timeoutCount >= 3 {
					return fmt.Errorf("timed out waiting for img copy progress")
				}
				continue
			}
			return err
		}
		timeoutCount = 0

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if progressV2, ok := parseProgressV2(trimmed); ok {
			flushProgressLine(&progressActive, &lastProgressWidth)
			printProgressTable(formatWorkerTable(progressV2), &lastTableLines)
			continue
		}
		if progress, ok := formatProgressDisplayLine(trimmed); ok {
			flushProgressTable(&lastTableLines)
			printProgressLine(progress, &lastProgressWidth)
			progressActive = true
			continue
		}
		flushProgressLine(&progressActive, &lastProgressWidth)
		flushProgressTable(&lastTableLines)
		if done, ok := formatDoneDisplayLine(trimmed); ok {
			printStreamLine(done)
			return nil
		}
		printStreamLine(trimmed)
		if strings.HasPrefix(trimmed, "img error:") {
			return nil
		}
	}
}

func printProgressLine(line string, lastWidth *int) {
	currentWidth := len(line)
	paddingWidth := 0
	if lastWidth != nil && *lastWidth > currentWidth {
		paddingWidth = *lastWidth - currentWidth
	}
	if paddingWidth > 0 {
		line += strings.Repeat(" ", paddingWidth)
	}
	_, _ = os.Stdout.WriteString("\r" + line)
	_ = os.Stdout.Sync()
	if lastWidth != nil {
		*lastWidth = currentWidth
	}
}

func flushProgressLine(active *bool, lastWidth *int) {
	if active == nil || !*active {
		return
	}
	_, _ = os.Stdout.WriteString("\n")
	_ = os.Stdout.Sync()
	*active = false
	if lastWidth != nil {
		*lastWidth = 0
	}
}

func printProgressTable(table string, lastLineCount *int) {
	lines := strings.Split(table, "\n")
	if len(lines) == 0 {
		return
	}

	lastCount := 0
	if lastLineCount != nil {
		lastCount = *lastLineCount
	}

	if supportsANSIOutput() {
		if lastCount > 0 {
			_, _ = os.Stdout.WriteString("\r\x1b[2K")
			for i := 1; i < lastCount; i++ {
				_, _ = os.Stdout.WriteString("\x1b[1A\r\x1b[2K")
			}
		}
	} else if lastCount > 0 {
		_, _ = os.Stdout.WriteString("\n")
	}

	for i, line := range lines {
		if i > 0 {
			_, _ = os.Stdout.WriteString("\n")
		}
		_, _ = os.Stdout.WriteString(line)
	}
	_ = os.Stdout.Sync()

	if lastLineCount != nil {
		*lastLineCount = len(lines)
	}
}

func flushProgressTable(lastLineCount *int) {
	if lastLineCount == nil || *lastLineCount == 0 {
		return
	}
	_, _ = os.Stdout.WriteString("\n")
	_ = os.Stdout.Sync()
	*lastLineCount = 0
}

func supportsANSIOutput() bool {
	ansiOutputSupport.once.Do(func() {
		info, err := os.Stdout.Stat()
		if err != nil {
			ansiOutputSupport.enabled = false
			return
		}
		if info.Mode()&os.ModeCharDevice == 0 {
			ansiOutputSupport.enabled = false
			return
		}
		term := strings.TrimSpace(strings.ToLower(os.Getenv("TERM")))
		ansiOutputSupport.enabled = term != "dumb"
	})
	return ansiOutputSupport.enabled
}

func printStreamLine(line string) {
	_, _ = os.Stdout.WriteString(line + "\n")
	_ = os.Stdout.Sync()
}

func isStreamImgCopyCommand(command string) bool {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) < 3 {
		return false
	}
	if !strings.EqualFold(fields[0], "img") || !strings.EqualFold(fields[1], "copy") {
		return false
	}
	for _, field := range fields[2:] {
		if strings.EqualFold(field, "--stream") {
			return true
		}
	}
	return false
}

func handleStreamLine(line string, progressActive *bool, lastProgressWidth *int, lastTableLines *int, streamState *int32) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return
	}
	if progressV2, ok := parseProgressV2(trimmed); ok {
		flushProgressLine(progressActive, lastProgressWidth)
		printProgressTable(formatWorkerTable(progressV2), lastTableLines)
		return
	}
	if progress, ok := formatProgressDisplayLine(trimmed); ok {
		flushProgressTable(lastTableLines)
		printProgressLine(progress, lastProgressWidth)
		if progressActive != nil {
			*progressActive = true
		}
		return
	}
	flushProgressLine(progressActive, lastProgressWidth)
	flushProgressTable(lastTableLines)
	if done, ok := formatDoneDisplayLine(trimmed); ok {
		printStreamLine(done)
		if streamState != nil {
			atomic.StoreInt32(streamState, 0)
		}
		return
	}
	printStreamLine(trimmed)
	if strings.HasPrefix(trimmed, "img error:") {
		if streamState != nil {
			atomic.StoreInt32(streamState, 0)
		}
	}
}

func runExportFlow(reader *bufio.Reader) {
	fmt.Println("EXPORT mode: enter a time range or Q to exit.")
	conn, err := dialWithRetry("127.0.0.1:50024", 5, 2*time.Second)
	if err != nil {
		fmt.Println("connect failed:", err)
		return
	}
	defer conn.Close()

	resp, err := sendCommandAndWait(conn, "hello server")
	if err != nil {
		fmt.Println("handshake failed:", err)
		return
	}
	if strings.TrimSpace(resp) != "1" {
		fmt.Println("server not ready:", resp)
		return
	}

	for {
		fmt.Print("Time range (YYYYMMDDHHMM-HHMM) or Q to exit: ")
		rangeInput, err := readLine(reader)
		if err != nil {
			fmt.Println("read failed:", err)
			return
		}
		if strings.EqualFold(rangeInput, "Q") {
			return
		}
		if rangeInput == "" {
			continue
		}

		countResp, err := sendCommandAndWait(conn, "img count "+rangeInput)
		if err != nil {
			fmt.Println("img count failed:", err)
			continue
		}
		fmt.Println(countResp)

		fmt.Print("Copy these images? (Y/N, Q to exit): ")
		answer, err := readLine(reader)
		if err != nil {
			fmt.Println("read failed:", err)
			return
		}
		if strings.EqualFold(answer, "Q") {
			return
		}
		if !strings.EqualFold(answer, "Y") {
			continue
		}

		fmt.Print("Destination dir (blank for ./img_dump): ")
		dest, err := readLine(reader)
		if err != nil {
			fmt.Println("read failed:", err)
			return
		}
		command := "img copy " + rangeInput
		command = command + " --stream"
		if strings.TrimSpace(dest) != "" {
			command = command + " " + dest
		}
		err = sendCommand(conn, command)
		if err != nil {
			fmt.Println("img copy failed:", err)
			continue
		}
		if err := streamCopyResponse(conn); err != nil {
			fmt.Println("img copy stream failed:", err)
			continue
		}
	}
}

func main() {
	Global_sig_current_run = 0
	title_print()
	input_channel := make(chan string)
	go func() {
		input := bufio.NewReader(os.Stdin)
		for {
			s, _ := input.ReadString('\n')
			s = strings.TrimSpace(s)

			if strings.ToUpper(s) == "Q" {
				Global_sig_lock.Lock()
				Global_sig_run = 0
				Global_sig_lock.Unlock()
				Global_writer_end = 1
				Global_sig_current_run_lock.Lock()
				Global_sig_current_run = 0
				Global_sig_current_run_lock.Unlock()
				return
			}
			if strings.ToUpper(s) == "NAN" {
				continue
			}
			if strings.ToUpper(s) == "START" {
				if Global_sig_current_run == 1 {
					fmt.Println("ss.exe is running")
					continue
				}
				// start ss.exe from os
				fmt.Println("start ss.exe")
				cmd := exec.Command("../screenshot/ss.exe")
				rootdir := "../screenshot/"
				cmd.Dir = rootdir
				//execute cmd
				/*
					cmd.SysProcAttr = &syscall.SysProcAttr{
						CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP, // Windows 下创建新进程组
					}

					cmd.Stdin = nil
					cmd.Stdout = nil
					cmd.Stderr = nil
				*/
				err := cmd.Start()

				if err != nil {
					fmt.Println("start ss.exe failed: ", err)
					continue
				}
				fmt.Println("start ss.exe success")
				continue

			}
			if strings.ToUpper(s) == "EXPORT" {
				runExportFlow(input)
				continue
			}

			if Global_sig_current_run == 0 { // must at last!
				fmt.Println("ss.exe is not running")
				continue
			}
			input_channel <- s
		}
	}()
	for {

		fmt.Println("Waiting for server...")
		//connect to server
	ini_link:
		task_net_dial := func(args ...interface{}) (interface{}, error) {
			conn, err := net.Dial("tcp", args[0].(string))
			return conn, err
		}
		conn := retry_task(task_net_dial, false, "127.0.0.1:50024").(net.Conn)
		Global_sig_current_run_lock.Lock()
		Global_sig_current_run = 1
		Global_sig_current_run_lock.Unlock()
		fmt.Println("connect established")
		for { // conn loop
			conn.Write([]byte("hello server"))
			var buf [1024]byte
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, err := conn.Read(buf[:])
			if err != nil {
				if opErr, ok := err.(net.Error); ok && opErr.Timeout() {
					if Global_sig_run == 0 {
						conn.Close()
						return
					}
					continue
				}
				fmt.Println("Error reading from connection:", err)
				Global_sig_current_run_lock.Lock()
				Global_sig_current_run = 0
				Global_sig_current_run_lock.Unlock()
				time.Sleep(5 * time.Second)
				goto ini_link
			}
			if string(buf[:n]) == "1" {
				fmt.Println("connect success")
				break
			}
		}
		wg := sync.WaitGroup{}
		var streamState int32
		wg.Add(2)
		// two go routine, one for send message, one for receive message
		go func() {
			defer wg.Done()
			for {
				if Global_sig_current_run == 0 {
					return
				}
				if Global_sig_run == 0 {
					return
				}
				select {
				case s := <-input_channel:
					if isStreamImgCopyCommand(s) {
						atomic.StoreInt32(&streamState, 1)
					}
					//send message to server
					_, err := conn.Write([]byte(s))
					if err != nil {
						atomic.StoreInt32(&streamState, 0)
						fmt.Printf("send failed,err:%v\n", err)
						continue
					}
				case <-time.After(5 * time.Second):
					continue
				}

			}
		}()
		go func() {
			defer wg.Done()
			streamPending := ""
			lastProgressWidth := 0
			progressActive := false
			lastTableLines := 0
			defer flushProgressLine(&progressActive, &lastProgressWidth)
			defer flushProgressTable(&lastTableLines)
			for {
				if Global_sig_run == 0 {
					return
				}
				var buf [1024]byte
				conn.SetReadDeadline(time.Now().Add(5 * time.Second))
				n, err := conn.Read(buf[:])
				if err != nil {
					if opErr, ok := err.(net.Error); ok && opErr.Timeout() {
						if atomic.LoadInt32(&streamState) == 1 {
							continue
						}
						continue
					}
					flushProgressLine(&progressActive, &lastProgressWidth)
					flushProgressTable(&lastTableLines)
					fmt.Println("Error reading from connection:", err)
					Global_sig_current_run_lock.Lock()
					Global_sig_current_run = 0
					Global_sig_current_run_lock.Unlock()
					return
				}

				chunk := string(buf[:n])
				if atomic.LoadInt32(&streamState) == 1 {
					streamPending += chunk
					for {
						idx := strings.IndexByte(streamPending, '\n')
						if idx < 0 {
							break
						}
						line := streamPending[:idx]
						streamPending = streamPending[idx+1:]
						handleStreamLine(line, &progressActive, &lastProgressWidth, &lastTableLines, &streamState)
					}
					continue
				}

				if streamPending != "" {
					handleStreamLine(streamPending, &progressActive, &lastProgressWidth, &lastTableLines, nil)
					streamPending = ""
				}
				flushProgressTable(&lastTableLines)

				msg := strings.TrimSpace(chunk)
				if msg == "" {
					continue
				}
				printStreamLine("Get server message: " + msg)
				if msg == "server close" {
					fmt.Printf("server closed\n")
					Global_sig_current_run_lock.Lock()
					Global_sig_current_run = 0
					Global_sig_current_run_lock.Unlock()
					return
				}
			}
		}()
		wg.Wait()
		conn.Close()
		if Global_sig_run == 0 {
			for {
				time.Sleep(1 * time.Second)
				if Global_writer_end == 1 {
					return
				}
			}
		}
	}
}
