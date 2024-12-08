package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var Global_sig_run int = 1
var Global_sig_lock sync.Mutex
var Global_sig_current_run int = 1
var Global_sig_current_run_lock sync.Mutex
var Global_writer_end int = 0

type Task func(args ...interface{}) (interface{}, error)

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

func main() {
	title_print()
	input_channel := make(chan string)
	go func() {
		input := bufio.NewReader(os.Stdin)
		for {
			s, _ := input.ReadString('\n')
			s = strings.TrimSpace(s)
			if Global_sig_current_run == 0 {
				fmt.Println("ss.exe is not running")
				continue
			}
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
		conn := retry_task(task_net_dial, false, "127.0.0.1:50021").(net.Conn)
		Global_sig_current_run_lock.Lock()
		Global_sig_current_run = 1
		Global_sig_current_run_lock.Unlock()
		fmt.Println("connect established")
		for {
			conn.Write([]byte("hello server"))
			var buf [1024]byte
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, err := conn.Read(buf[:])
			if err != nil {
				if opErr, ok := err.(net.Error); ok && opErr.Timeout() {
					continue
				}
				fmt.Println("Error reading from connection:", err)
				time.Sleep(5 * time.Second)
				goto ini_link
			}
			if string(buf[:n]) == "1" {
				fmt.Println("connect success")
				break
			}
		}
		wg := sync.WaitGroup{}
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
					//send message to server
					_, err := conn.Write([]byte(s))
					if err != nil {
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
			for {
				if Global_sig_run == 0 {
					return
				}
				var buf [1024]byte
				conn.SetReadDeadline(time.Now().Add(5 * time.Second))
				n, err := conn.Read(buf[:])
				if err != nil {
					if opErr, ok := err.(net.Error); ok && opErr.Timeout() {
						continue
					}
					fmt.Println("Error reading from connection:", err)
					Global_sig_current_run_lock.Lock()
					Global_sig_current_run = 0
					Global_sig_current_run_lock.Unlock()
					return
				}
				fmt.Println("Get server message: ", string(buf[:n]))
				if string(buf[:n]) == "server close" {
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
