package main

import (
	"bufio"
	"fmt"
	"net"
	"time"
)

const (
	serverHost   = "localhost" // 服务器地址
	serverPort   = 8080        // 服务器端口
	clientNum    = 5           // 客户端数量
	sendInterval = 30          // 发送间隔（秒）
)

func main() {
	// 创建多个客户端
	clients := make([]net.Conn, 0)
	msgChans := make([]chan string, 0)

	// 启动多个客户端
	for i := 0; i < clientNum; i++ {
		conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", serverHost, serverPort))
		if err != nil {
			fmt.Printf("客户端 %d 连接失败: %v\n", i+1, err)
			continue
		}
		clients = append(clients, conn)
		msgChan := make(chan string)
		msgChans = append(msgChans, msgChan)

		// 启动接收消息的 goroutine
		go func(clientNum int, conn net.Conn, msgChan chan string) {
			reader := bufio.NewReader(conn)
			for {
				response, err := reader.ReadString('\n')
				if err != nil {
					fmt.Printf("客户端 %d 读取失败: %v\n", clientNum, err)
					return
				}
				msgChan <- fmt.Sprintf("客户端 %d 收到: %s", clientNum, response)
			}
		}(i+1, conn, msgChan)

		// 启动发送消息的 goroutine
		go func(clientNum int, conn net.Conn) {
			ticker := time.NewTicker(time.Duration(sendInterval) * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					message := fmt.Sprintf("客户端 %d 的自动消息", clientNum)
					_, err := conn.Write([]byte(message + "\n"))
					if err != nil {
						fmt.Printf("客户端 %d 发送失败: %v\n", clientNum, err)
						return
					}
				}
			}
		}(i+1, conn)

		fmt.Printf("客户端 %d 已连接到服务器 %s:%d\n", i+1, serverHost, serverPort)
	}

	// 主循环只负责显示消息
	for {
		// 显示所有客户端的消息
		for _, msgChan := range msgChans {
			select {
			case msg := <-msgChan:
				fmt.Println(msg)
			default:
			}
		}
		time.Sleep(100 * time.Millisecond) // 避免CPU占用过高
	}
}
