//go:build darwin

package main

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

const (
	port        = 8080
	maxReadSize = 1024
	maxEvents   = 100 // 每次从 kqueue 获取事件的最大返回数量
	numWorkers  = 100 // 工作 goroutine 的数量
)

// 定义任务结构
type task struct {
	fd int
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
	addr := syscall.SockaddrInet4{Port: port}
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

	fmt.Printf("服务器启动在端口 %d\n", port)

	// 创建 kqueue
	kq, err := syscall.Kqueue()
	if err != nil {
		fmt.Printf("创建 kqueue 失败: %v\n", err)
		return
	}
	defer func() {
		if err := syscall.Close(kq); err != nil {
			fmt.Printf("关闭 kqueue 失败: %v\n", err)
		}
	}()

	// 将监听 socket 添加到 kqueue
	changes := []syscall.Kevent_t{
		{
			Ident:  uint64(listenFd),
			Filter: syscall.EVFILT_READ,
			Flags:  syscall.EV_ADD,
		},
	}
	_, err = syscall.Kevent(kq, changes, nil, nil)
	if err != nil {
		fmt.Printf("添加监听事件到 kqueue 失败: %v\n", err)
		return
	}

	// 创建任务队列和 goroutine 池
	taskChan := make(chan task, 10000) // 更大的任务队列

	// 启动工作池
	for i := 0; i < numWorkers; i++ {
		go worker(kq, listenFd, taskChan)
	}

	// 事件循环
	events := make([]syscall.Kevent_t, maxEvents)
	for {
		// 等待事件
		n, err := syscall.Kevent(kq, nil, events, nil)
		if err != nil {
			fmt.Printf("等待事件失败: %v\n", err)
			continue
		}

		// 将事件分发给工作池
		for i := 0; i < n; i++ {
			fd := int(events[i].Ident)
			taskChan <- task{fd: fd}
		}
	}
}

// 处理新连接
func handleNewConnection(kq, listenFd int) {
	clientFd, _, err := syscall.Accept(listenFd)
	if err != nil {
		fmt.Printf("接受连接失败: %v\n", err)
		return
	}

	// 设置非阻塞模式
	err = syscall.SetNonblock(clientFd, true)
	if err != nil {
		fmt.Printf("设置非阻塞模式失败: %v\n", err)
		if err := syscall.Close(clientFd); err != nil {
			fmt.Printf("关闭客户端 socket 失败: %v\n", err)
		}
		return
	}

	// 将新连接的 socket 添加到 kqueue
	changes := []syscall.Kevent_t{
		{
			Ident:  uint64(clientFd),
			Filter: syscall.EVFILT_READ,
			Flags:  syscall.EV_ADD,
		},
	}
	if _, err := syscall.Kevent(kq, changes, nil, nil); err != nil {
		fmt.Printf("添加客户端事件到 kqueue 失败: %v\n", err)
		if err := syscall.Close(clientFd); err != nil {
			fmt.Printf("关闭客户端 socket 失败: %v\n", err)
		}
		return
	}

	fmt.Printf("新客户端连接: %d\n", clientFd)
}

// 读取客户端数据
func readClientData(fd int) ([]byte, error) {
	buf := make([]byte, maxReadSize)
	n, err := syscall.Read(fd, buf)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil // 客户端关闭连接
	}
	return buf[:n], nil
}

// 发送响应给客户端
func sendResponse(fd int, message string) error {
	response := fmt.Sprintf("服务器已收到消息: %s", message)
	_, err := syscall.Write(fd, []byte(response))
	return err
}

// 关闭客户端连接
func closeClientConnection(kq, fd int) {
	// 移除 kqueue 事件
	changes := []syscall.Kevent_t{
		{
			Ident:  uint64(fd),
			Filter: syscall.EVFILT_READ,
			Flags:  syscall.EV_DELETE,
		},
	}
	if _, err := syscall.Kevent(kq, changes, nil, nil); err != nil {
		fmt.Printf("移除 kqueue 事件失败: %v\n", err)
	}
	// 关闭连接
	if err := syscall.Close(fd); err != nil {
		fmt.Printf("关闭客户端 socket 失败: %v\n", err)
	}
}

// 处理客户端事件
func handleClientEvent(kq, fd int) {
	// 读取数据
	data, err := readClientData(fd)
	if err != nil {
		if !errors.Is(err, syscall.EAGAIN) {
			fmt.Printf("读取数据失败: %v\n", err)
			closeClientConnection(kq, fd)
		}
		return
	}

	if data == nil {
		// 客户端关闭连接
		fmt.Printf("客户端断开连接: %d\n", fd)
		closeClientConnection(kq, fd)
		return
	}

	// 处理接收到的数据
	message := string(data)
	fmt.Printf("收到来自客户端 %d 的消息: %s\n", fd, message)

	// 发送响应
	if err = sendResponse(fd, message); err != nil {
		fmt.Printf("发送响应失败: %v\n", err)
		closeClientConnection(kq, fd)
	}
}

// 工作池处理函数
func worker(kq, listenFd int, taskChan chan task) {
	for t := range taskChan {
		fd := t.fd
		if fd == listenFd {
			handleNewConnection(kq, listenFd)
		} else {
			handleClientEvent(kq, fd)
		}
	}
}
