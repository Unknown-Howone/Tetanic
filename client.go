package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

const (
	SERVER_HOST = "localhost" // 服务器地址
	SERVER_PORT = 8080        // 服务器端口
)

func main() {
	// 连接到服务器
	serverAddr := fmt.Sprintf("%s:%d", SERVER_HOST, SERVER_PORT)
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		fmt.Printf("连接服务器失败: %v\n", err)
		return
	}
	defer conn.Close()

	fmt.Println("已连接到服务器，请输入消息（输入 'quit' 退出）")

	// 创建一个 channel 用于同步消息显示
	msgChan := make(chan string)

	// 启动一个 goroutine 用于接收服务器响应
	go func() {
		for {
			// 读取服务器响应
			response, err := bufio.NewReader(conn).ReadString('\n')
			if err != nil {
				fmt.Printf("读取服务器响应失败: %v\n", err)
				return
			}
			msgChan <- response
		}
	}()

	// 主循环：读取用户输入并发送到服务器
	reader := bufio.NewReader(os.Stdin)
	for {
		// 读取用户输入
		fmt.Print("请输入消息: ")
		message, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("读取输入失败: %v\n", err)
			continue
		}

		// 去除换行符
		message = strings.TrimSpace(message)

		// 检查是否退出
		if message == "quit" {
			fmt.Println("正在退出...")
			return
		}

		// 发送消息到服务器
		_, err = conn.Write([]byte(message + "\n"))
		if err != nil {
			fmt.Printf("发送消息失败: %v\n", err)
			continue
		}

		// 等待并显示服务器响应
		response := <-msgChan
		fmt.Printf("服务器响应: %s\n", response)
	}
}
