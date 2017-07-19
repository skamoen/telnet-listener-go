package main

import (
	"fmt"
	"net"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
)

func init() {
	log.SetFormatter(&log.JSONFormatter{
		FieldMap: log.FieldMap{
			log.FieldKeyTime: "@timestamp",
			// 		 FieldKeyLevel: "@level",
			// 		 FieldKeyMsg: "@message",
		},
	})
	log.SetOutput(os.Stdout)
	//   You could set this to any `io.Writer` such as a file
	// file, err := os.OpenFile("telnet-listener.log", os.O_CREATE|os.O_WRONLY, 0666)
	// if err == nil {
	// 	log.SetOutput(file)
	// } else {
	// 	log.Info("Failed to log to file, using default stderr")
	// }
	log.SetLevel(log.DebugLevel)
}

func main() {
	banner := []byte("\nUser Access Verification\r\nUsername:")
	timeout := 5 * time.Second

	ln, err := net.Listen("tcp", ":2324")
	checkError(err)

	log.Info("Server started on port 2324")

	for {
		conn, err := ln.Accept()
		checkError(err)
		go handleConnection(conn, banner, timeout)
	}
}

func handleConnection(conn net.Conn, banner []byte, timeout time.Duration) {
	defer conn.Close()

	connectionLog := log.WithFields(log.Fields{
		"remote_ip": conn.RemoteAddr().String(),
	})

	connectionLog.Info("Accepted connection")

	err := negotiateTelnet(conn)
	// If telnet negotiation fails, close the socket
	if err != nil {
		return
	}

	conn.Write(banner)

	var buf [64]byte

	for {
		conn.SetReadDeadline(time.Now().Add(timeout))
		// read upto 512 bytes
		n, err := conn.Read(buf[0:])
		checkError(err)

		fmt.Println("read:", buf[0:n])

		// write the n bytes read
		_, err2 := conn.Write(buf[0:n])
		if err2 != nil {
			return
		}
	}

}

func negotiateTelnet(conn net.Conn) (err error) {
	// Negotiate Telnet parameters
	telnetCommands := []byte{255, 253, 34, 255, 251, 1}
	// Handle connection
	conn.Write(telnetCommands)

	commandEcho := false
	commandLinemode := false

	for {
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		var buffer [3]byte
		_, err := conn.Read(buffer[0:])
		fmt.Println("read:", buffer)

		if err != nil {
			return err
		}

		if buffer[0] == 255 {
			if buffer[1] == 253 || buffer[1] == 251 || buffer[1] == 252 || buffer[1] == 254 {
				if buffer[2] == 1 {
					commandEcho = true
				}
				if buffer[2] == 34 {
					commandLinemode = true
				}
			}
		}
		if commandEcho && commandLinemode {
			fmt.Println("Got both commands")
			break
		}
	}
}

func checkError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fatal error: %s", err.Error())
		os.Exit(1)
	}
}
