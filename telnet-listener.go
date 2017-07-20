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
	sessionCounter := 1

	ln, err := net.Listen("tcp", ":2324")
	exitOnError(err)

	log.Info("Server started on %s", ln.Addr().String())

	for {
		conn, err := ln.Accept()
		exitOnError(err)
		// Accept the connection and launch a routine for handling
		go handleConnection(conn, banner, timeout, sessionCounter)
	}
}

func handleConnection(conn net.Conn, banner []byte, timeout time.Duration, sessionCounter int) {
	defer conn.Close()

	// Log all connection related events with remote logging
	connectionLog := log.WithFields(log.Fields{
		"remote_addr":   conn.RemoteAddr().String(),
		"type":          "connection",
		"session_count": sessionCounter,
	})

	connectionLog.Info("Accepted connection")

	err := negotiateTelnet(conn)
	// If telnet negotiation fails, close the socket
	// TODO: Accept a raw connection, as several clients aren't actually telnet
	if err != nil {
		connectionLog.Error("Telnet commands failed")
		return
	}

	conn.Write(banner)

	var buf [1]byte

	for {
		conn.SetReadDeadline(time.Now().Add(timeout))
		// read upto 512 bytes
		n, err := conn.Read(buf[0:])
		if err != nil {
			// Read error, most likely time-out
			connectionLog.Warn("Read error, closing socket")
			return
		}

		connectionLog.WithFields(log.Fields{
			"type":       "input",
			"input_byte": buf[0],
		}).Info("Input received")

		fmt.Println("read:", buf[0:n])

		switch buf[0] {
		case 127: // DEL
			fallthrough
		case 8: // Backspace
			conn.Write([]byte("\b \b"))
		case 0: // null
			fallthrough
		case 10: // New Line
			// handleNewline(out);
		case 13:
		default:
			_, err2 := conn.Write(buf[0:n])
			if err2 != nil {
				return
			}
		}
	}
}

func handleNewline(conn net.Conn) {

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
		// Read 3 bytes per read for commands
		var buffer [3]byte
		_, err := conn.Read(buffer[0:])
		// fmt.Println("read:", buffer)

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
			break
		}
	}
	return nil
}

func exitOnError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fatal error: %s", err.Error())
		os.Exit(1)
	}
}
