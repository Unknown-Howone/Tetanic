//go:build linux

package main

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

const (
	PORT             = 8080 // 服务器端口
	MAX_READ_SIZE    = 1024 // 单次读取大小
	WORKER_POOL_SIZE = 100  // 工作池大小
)

// 工作池任务
type task struct {
	fd   int
	data []byte
}

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
	epfd, err := syscall.EpollCreate(10) // 参数 size 在 Linux 2.6.8 之后被忽略，但必须大于 0
	if err != nil {
		fmt.Printf("创建 epoll 失败: %v\n", err)
		return
	}
	defer func() {
		if err := syscall.Close(epfd); err != nil {
			fmt.Printf("关闭 epoll 失败: %v\n", err)
		}
	}()

	// 将监听 socket 添加到 epoll，使用水平触发模式
	event := syscall.EpollEvent{
		Events: syscall.EPOLLIN, // 水平触发模式
		Fd:     int32(listenFd),
	}
	err = syscall.EpollCtl(epfd, syscall.EPOLL_CTL_ADD, listenFd, &event)
	if err != nil {
		fmt.Printf("添加监听事件到 epoll 失败: %v\n", err)
		return
	}

	// 创建工作池
	taskChan := make(chan task, 1000) // 任务队列
	for i := 0; i < WORKER_POOL_SIZE; i++ {
		go worker(taskChan)
	}

	// 事件循环
	events := make([]syscall.EpollEvent, 100)
	for {
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

				// 设置非阻塞模式
				err = syscall.SetNonblock(clientFd, true)
				if err != nil {
					fmt.Printf("设置非阻塞模式失败: %v\n", err)
					if err := syscall.Close(clientFd); err != nil {
						fmt.Printf("关闭客户端 socket 失败: %v\n", err)
					}
					continue
				}

				// 将新连接的 socket 添加到 epoll，使用边缘触发模式
				clientEvent := syscall.EpollEvent{
					Events: syscall.EPOLLIN | syscall.EPOLLET, // 边缘触发模式
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
				var totalData []byte

				// 在边缘触发模式下，必须一次性读取所有数据
				for {
					buf := make([]byte, MAX_READ_SIZE)
					n, err := syscall.Read(clientFd, buf)
					if err != nil {
						if errors.Is(err, syscall.EAGAIN) {
							// 没有更多数据可读，退出循环
							break
						}
						fmt.Printf("读取数据失败: %v\n", err)
						if err := syscall.Close(clientFd); err != nil {
							fmt.Printf("关闭客户端 socket 失败: %v\n", err)
						}
						break
					}

					if n == 0 {
						// 客户端关闭连接
						if err := syscall.Close(clientFd); err != nil {
							fmt.Printf("关闭客户端 socket 失败: %v\n", err)
						}
						fmt.Printf("客户端断开连接: %d\n", clientFd)
						break
					}

					// 将读取的数据追加到总数据中
					totalData = append(totalData, buf[:n]...)
				}

				if len(totalData) > 0 {
					// 将任务提交到工作池
					taskChan <- task{
						fd:   clientFd,
						data: totalData,
					}
				}
			}
		}
	}
}

// 工作池处理函数
func worker(taskChan chan task) {
	for task := range taskChan {
		// 处理接收到的数据
		message := string(task.data)
		fmt.Printf("收到来自客户端 %d 的消息: %s\n", task.fd, message)

		// 发送响应
		response := fmt.Sprintf("服务器已收到消息: %s", message)
		_, err := syscall.Write(task.fd, []byte(response))
		if err != nil {
			fmt.Printf("发送响应失败: %v\n", err)
		}
	}
}
