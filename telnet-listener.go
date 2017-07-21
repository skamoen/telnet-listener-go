package main

import (
	"net"
	"os"
	"time"

	"bytes"

	"strings"

	log "github.com/sirupsen/logrus"
)

func init() {
	log.SetFormatter(&log.JSONFormatter{
		TimestampFormat: "2006-01-02T15:04:05.999Z07:00",
		FieldMap: log.FieldMap{
			log.FieldKeyTime: "@timestamp",
			// 		 FieldKeyLevel: "@level",
			// 		 FieldKeyMsg: "@message",
		},
	})
	// log.SetOutput(os.Stdout)
	//   You could set this to any `io.Writer` such as a file
	file, err := os.OpenFile("telnet-listener.log", os.O_CREATE|os.O_WRONLY, 0666)
	if err == nil {
		log.SetOutput(file)
	} else {
		log.Info("Failed to log to file, using default stderr")
	}
	log.SetLevel(log.DebugLevel)
}

func main() {
	banner := []byte("\nUser Access Verification\r\nUsername:")
	timeout := 30 * time.Second
	sessionCounter := 1

	ln, err := net.Listen("tcp", ":2323")
	if err != nil {
		log.WithError(err).Fatal("Starting listener failed")
		os.Exit(1)
	}

	log.Info("Server started on ", ln.Addr().String())

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.WithError(err).Warn("Can't accept socket")
		}
		// Accept the connection and launch a routine for handling
		go handleConnection(conn, banner, timeout, sessionCounter)
		sessionCounter++
	}
}

func handleConnection(conn net.Conn, banner []byte, timeout time.Duration, sessionCounter int) {
	defer conn.Close()

	// Save the current state, username and password
	state := [3]string{"username", "", ""}

	// Log all connection related events with remote logging
	connectionLog := log.WithFields(log.Fields{
		"remote_ip": conn.RemoteAddr().(*net.TCPAddr).IP,
		"port":      conn.RemoteAddr().(*net.TCPAddr).Port,
		"type":      "connection",
		"session":   sessionCounter,
	})

	connectionLog.Info("Accepted connection")

	sessionLifeTime := time.Now()
	defer logSessionTime(sessionLifeTime, connectionLog)

	// Set linemode and echo mode
	err := negotiateTelnet(conn)
	// If telnet negotiation fails, close the socket
	if err != nil {
		connectionLog.WithError(err).Error("Telnet commands failed")
		return
	}

	// Send the banner to the remote host
	conn.Write(banner)

	// Read one byte at a time
	var buf [1]byte
	var input bytes.Buffer
	lastInput := time.Now()

	for {
		conn.SetReadDeadline(time.Now().Add(timeout))

		n, err := conn.Read(buf[0:])
		if err != nil {
			// Read error, most likely time-out
			connectionLog.WithError(err).Warn("Read error")
			return
		}

		connectionLog.WithFields(log.Fields{
			"type":       "input",
			"input_byte": buf[0],
			"input_char": string(buf[0]),
			"last_input": time.Since(lastInput).Nanoseconds() / 1000000,
		}).Info("Input received")

		switch buf[0] {
		case 127: // DEL
			fallthrough
		case 8: // Backspace
			if input.Len() > 0 {
				// Remove the previous character from the buffer
				input.Truncate(input.Len() - 1)
				if state[0] == "username" {
					// Remove the character at the remote host
					conn.Write([]byte("\b \b"))
				}
			}
		case 0: // null
			fallthrough
		case 10: // New Line
			state = handleNewline(conn, state, &input, connectionLog)
		case 13:
		default:
			if state[0] == "username" {
				// Echo the character when in username mode
				_, err := conn.Write(buf[0:n])
				if err != nil {
					connectionLog.WithError(err).Error("Can't write to connection")
				}
			}
			// Store the input
			input.WriteByte(buf[0])
		}
		lastInput = time.Now()
	}
}

func handleNewline(conn net.Conn, state [3]string, input *bytes.Buffer, connectionLog *log.Entry) [3]string {
	if state[0] == "username" {
		// Read all characters in the buffer
		state[1] = input.String()
		connectionLog.WithField("username", state[1]).Info("Username entered")

		// Clear the buffer
		input.Reset()

		// Switch to password entry
		state[0] = "password"
		conn.Write([]byte("\r\nPassword: "))
	} else {
		// Store all characters in the buffer
		state[2] = input.String()

		connectionLog.WithFields(log.Fields{
			"password": state[2],
			"entry":    strings.Join(state[1:], ":"),
		}).Info("Password entered")

		// Reset the buffers and state
		input.Reset()
		state[0] = "username"
		state[1] = ""
		state[2] = ""

		conn.Write([]byte("\r\nWrong password!\r\n\r\nUsername: "))
	}
	return state
}

// Poor implemention of DO LINEMODE and WILL ECHO. If it's a normal telnet client, this works just fine
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

		if err != nil {
			return err
		}

		// IAC
		if buffer[0] == 255 {
			// DO, WILL, WONT, DONT
			if buffer[1] == 253 || buffer[1] == 251 || buffer[1] == 252 || buffer[1] == 254 {
				// ECHO
				if buffer[2] == 1 {
					commandEcho = true
				}
				// LINEMODE
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

func logSessionTime(start time.Time, log *log.Entry) {
	log.WithField("session_time", time.Since(start)/1000000).Info("Connection closed")
}
