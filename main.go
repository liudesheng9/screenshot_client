package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

var Global_sig_run int = 1
var Global_sig_lock sync.Mutex

func title_print() {
	fmt.Println(`____    ____  ____   _____  _____  _   _  ____   _   _   ___  _____ `)
	fmt.Println(`/ ___|  / ___||  _ \ | ____|| ____|| \ | |/ ___| | | | | / _ \|_   _|`)
	fmt.Println(`\___ \ | |    | |_) ||  _|  |  _|  |  \| |\___ \ | |_| || | | | | |  `)
	fmt.Println(` ___) || |___ |  _ < | |___ | |___ | |\  | ___) ||  _  || |_| | | |  `)
	fmt.Println(`|____/  \____||_| \_\|_____||_____||_| \_||____/ |_| |_| \___/  |_|  `)
	fmt.Println("CLIENT")
}

func main() {
	title_print()
	fmt.Println("Waiting for server...")
	for {
		//connect to server
		conn, err := net.Dial("tcp", "127.0.0.1:50021")
		if err != nil {
			// fmt.Printf("dial failed, err:%v\n", err)
			time.Sleep(5 * time.Second)
			continue
		}

		conn.Write([]byte("hello server"))
		var buf [1024]byte
		n, err := conn.Read(buf[:])
		if err != nil {
			fmt.Printf("read failed, err:%v\n", err)
			continue
		}
		if string(buf[:n]) == "1" {
			fmt.Println("connect success")
		}
		wg := sync.WaitGroup{}
		wg.Add(2)
		// two go routine, one for send message, one for receive message
		input := bufio.NewReader(os.Stdin)
		go func() {
			defer wg.Done()
			for {
				s, _ := input.ReadString('\n')
				s = strings.TrimSpace(s)
				if strings.ToUpper(s) == "Q" {
					Global_sig_lock.Lock()
					Global_sig_run = 0
					Global_sig_lock.Unlock()
					return
				}
				//send message to server
				_, err := conn.Write([]byte(s))
				if err != nil {
					fmt.Printf("send failed,err:%v\n", err)
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
					continue
				}
				fmt.Println("Get server message: ", string(buf[:n]))
				if string(buf[:n]) == "server close" {
					fmt.Printf("server closed\n")
					return
				}
			}
		}()
		wg.Wait()
		conn.Close()
		if Global_sig_run == 0 {
			break
		}
	}
}
