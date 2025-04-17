//go:build linux

package main

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

const (
	PORT = 8080 // 服务器端口
)

func main() {
	// 创建监听 socket
	listenFd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		fmt.Printf("创建 socket 失败: %v\n", err)
		return
	}
	defer func() {
		if err := syscall.Close(listenFd); err != nil {
			fmt.Printf("关闭监听 socket 失败: %v\n", err)
		}
	}()

	// 设置 socket 选项
	err = syscall.SetsockoptInt(listenFd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	if err != nil {
		fmt.Printf("设置 socket 选项失败: %v\n", err)
		return
	}

	// 绑定地址
	addr := syscall.SockaddrInet4{Port: PORT}
	copy(addr.Addr[:], net.ParseIP("0.0.0.0").To4())
	err = syscall.Bind(listenFd, &addr)
	if err != nil {
		fmt.Printf("绑定地址失败: %v\n", err)
		return
	}

	// 开始监听
	err = syscall.Listen(listenFd, 10)
	if err != nil {
		fmt.Printf("监听失败: %v\n", err)
		return
	}

	fmt.Printf("服务器启动在端口 %d\n", PORT)

	// 创建 epoll
	epfd, err := syscall.EpollCreate1(0)
	if err != nil {
		fmt.Printf("创建 epoll 失败: %v\n", err)
		return
	}
	defer func() {
		if err := syscall.Close(epfd); err != nil {
			fmt.Printf("关闭 epoll 失败: %v\n", err)
		}
	}()

	// 将监听 socket 添加到 epoll
	event := syscall.EpollEvent{
		Events: syscall.EPOLLIN,
		Fd:     int32(listenFd),
	}
	err = syscall.EpollCtl(epfd, syscall.EPOLL_CTL_ADD, listenFd, &event)
	if err != nil {
		fmt.Printf("添加监听事件到 epoll 失败: %v\n", err)
		return
	}

	// 事件循环
	events := make([]syscall.EpollEvent, 100)
	for {
		// 等待事件
		n, err := syscall.EpollWait(epfd, events, -1)
		if err != nil {
			fmt.Printf("等待事件失败: %v\n", err)
			continue
		}

		for i := 0; i < n; i++ {
			if int(events[i].Fd) == listenFd {
				// 监听 socket 有事件，说明有新连接
				clientFd, _, err := syscall.Accept(listenFd)
				if err != nil {
					fmt.Printf("接受连接失败: %v\n", err)
					continue
				}

				// 将新连接的 socket 添加到 epoll
				clientEvent := syscall.EpollEvent{
					Events: syscall.EPOLLIN,
					Fd:     int32(clientFd),
				}
				err = syscall.EpollCtl(epfd, syscall.EPOLL_CTL_ADD, clientFd, &clientEvent)
				if err != nil {
					fmt.Printf("添加客户端事件到 epoll 失败: %v\n", err)
					if err := syscall.Close(clientFd); err != nil {
						fmt.Printf("关闭客户端 socket 失败: %v\n", err)
					}
					continue
				}

				fmt.Printf("新客户端连接: %d\n", clientFd)
			} else {
				// 处理客户端 socket 的事件
				clientFd := int(events[i].Fd)
				buf := make([]byte, 1024)
				n, err := syscall.Read(clientFd, buf)
				if err != nil {
					if !errors.Is(err, syscall.EAGAIN) {
						fmt.Printf("读取数据失败: %v\n", err)
					}
					continue
				}

				if n == 0 {
					// 客户端关闭连接
					if err := syscall.Close(clientFd); err != nil {
						fmt.Printf("关闭客户端 socket 失败: %v\n", err)
					}
					fmt.Printf("客户端断开连接: %d\n", clientFd)
					continue
				}

				// 处理接收到的数据
				message := string(buf[:n])
				fmt.Printf("收到来自客户端 %d 的消息: %s\n", clientFd, message)

				// 发送响应
				response := fmt.Sprintf("服务器已收到消息: %s", message)
				_, err = syscall.Write(clientFd, []byte(response))
				if err != nil {
					fmt.Printf("发送响应失败: %v\n", err)
				}
			}
		}
	}
}
