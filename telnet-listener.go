package main

import (
	"net"
	"os"
	"time"

	"bytes"

	"strings"

	"flag"
	"strconv"

	log "github.com/sirupsen/logrus"
)

var (
	numberOfPorts  int
	portStart      int
	timeoutSetting time.Duration
	sessionLog     *log.Logger
)

func init() {
	devPtr := flag.Bool("dev", false, "Enable stdOut development logging")
	flag.IntVar(&portStart, "p", 23000, "Port to listen on")
	flag.IntVar(&numberOfPorts, "n", 1, "Number of ports to listen on")
	flag.DurationVar(&timeoutSetting, "t", 30, "Time to wait for idle connections")

	flag.Parse()

	sessionLog = &log.Logger{
		Formatter: new(log.JSONFormatter),
		Hooks:     make(log.LevelHooks),
		Level:     log.DebugLevel,
	}

	if *devPtr {
		log.SetOutput(os.Stdout)
		sessionLog.Out = os.Stdout
	} else {
		sessionLog.Formatter = &log.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.999Z07:00",
			FieldMap: log.FieldMap{
				log.FieldKeyTime: "@timestamp",
				// 		 FieldKeyLevel: "@level",
				// 		 FieldKeyMsg: "@message",
			},
		}

		log.SetFormatter(&log.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.999Z07:00",
			FieldMap: log.FieldMap{
				log.FieldKeyTime: "@timestamp",
				// 		 FieldKeyLevel: "@level",
				// 		 FieldKeyMsg: "@message",
			},
		})
		//   You could set this to any `io.Writer` such as a file
		file, err := os.OpenFile("telnet-listener.log", os.O_CREATE|os.O_WRONLY, 0666)
		if err == nil {
			log.SetOutput(file)
		} else {
			log.Info("Failed to log to file, using default stderr")
		}

		sessionfile, err := os.OpenFile("telnet-session.log", os.O_CREATE|os.O_WRONLY, 0666)
		if err == nil {
			sessionLog.Out = sessionfile

		} else {
			log.Info("Failed to log to file, using default stderr")
		}
	}

	log.SetLevel(log.DebugLevel)
}

func main() {
	banner := []byte("\nUser Access Verification\r\nUsername:")
	timeout := timeoutSetting * time.Second
	sCount := 1
	cchan := make(chan net.Conn, 100)

	// Open the specified number of ports from
	for i := portStart; i < portStart+numberOfPorts; i++ {
		go func(cchan chan net.Conn, i int) {
			ln, err := net.Listen("tcp", ":"+strconv.Itoa(i))
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
				cchan <- conn
			}
		}(cchan, i)
	}

	for {
		conn := <-cchan
		// Accept the connection and launch a routine for handling
		go handleConnection(conn, banner, timeout, sCount)
		sCount++
	}
}

func handleConnection(conn net.Conn, banner []byte, timeout time.Duration, sCount int) {
	defer conn.Close()

	// Save the current state, username and password
	state := [3]string{"username", "", ""}

	// Log all connection related events with remote logging
	conlog := log.WithFields(log.Fields{
		"remote_ip":   conn.RemoteAddr().(*net.TCPAddr).IP,
		"remote_port": conn.RemoteAddr().(*net.TCPAddr).Port,
		"local_port":  conn.LocalAddr().(*net.TCPAddr).Port,
		"type":        "connection",
		"session":     sCount,
	})

	metrics := new(metrics)
	metrics.sessionStart = time.Now()
	defer metrics.log()

	metrics.sessionID = sCount
	metrics.remoteIP = conn.RemoteAddr().(*net.TCPAddr).IP
	metrics.remotePort = conn.RemoteAddr().(*net.TCPAddr).Port
	metrics.localPort = conn.LocalAddr().(*net.TCPAddr).Port

	conlog.Info("Accepted connection")

	t := time.Now()
	defer logSessionTime(t, conlog)

	// Set linemode and echo mode
	err := negotiateTelnet(conn)
	// If telnet negotiation fails, close the socket
	if err != nil {
		conlog.WithError(err).Error("Telnet commands failed")
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
			conlog.WithError(err).Warn("Read error")
			return
		}

		// conlog.WithFields(log.Fields{
		// 	"type":       "input",
		// 	"input_byte": buf[0],
		// 	"input_char": string(buf[0]),
		// 	"last_input": time.Since(lastInput).Nanoseconds() / 1000000,
		// }).Info("Input received")

		metrics.input = append(metrics.input, buf[0])
		metrics.inputTimes = append(metrics.inputTimes, time.Since(lastInput).Nanoseconds()/1000000)

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
			state = handleNewline(conn, state, &input, metrics)
		case 13:
		default:
			if state[0] == "username" {
				// Echo the character when in username mode
				_, err := conn.Write(buf[0:n])
				if err != nil {
					conlog.WithError(err).Error("Can't write to connection")
				}
			}
			// Store the input
			input.WriteByte(buf[0])
		}
		lastInput = time.Now()
	}
}

func handleNewline(conn net.Conn, state [3]string, input *bytes.Buffer, metrics *metrics) [3]string {
	if state[0] == "username" {
		// Read all characters in the buffer
		state[1] = input.String()
		metrics.usernames = append(metrics.usernames, state[1])
		// conlog.WithField("username", state[1]).Info("Username entered")

		// Clear the buffer
		input.Reset()

		// Switch to password entry
		state[0] = "password"
		conn.Write([]byte("\r\nPassword: "))
	} else {
		// Store all characters in the buffer
		state[2] = input.String()

		metrics.passwords = append(metrics.passwords, state[2])
		metrics.entries = append(metrics.entries, strings.Join(state[1:], ":"))
		// conlog.WithFields(log.Fields{
		// 	"password": state[2],
		// 	"entry":    strings.Join(state[1:], ":"),
		// }).Info("Password entered")

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
	// Write IAC DO LINE MODE IAC WILL ECH
	conn.Write([]byte{255, 253, 34, 255, 251, 1})

	// Expect IAC DO ECHO
	ce := false
	// Expect IAC WILL LINEMODE
	cl := false

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
					ce = true
				}
				// LINEMODE
				if buffer[2] == 34 {
					cl = true
				}
			}
		}
		if ce && cl {
			break
		}
	}
	return nil
}

func (m *metrics) log() {

	sessionLog.WithFields(log.Fields{
		"session_id":       m.sessionID,
		"session_start":    m.sessionStart,
		"session_duration": time.Since(m.sessionStart) / 1000000,
		"remote_ip":        m.remoteIP,
		"local_port":       m.localPort,
		"remote_port":      m.remotePort,
		"input_bytes":      m.input,
		"input_times":      m.inputTimes,
		"usernames":        m.usernames,
		"passwords":        m.passwords,
		"entries":          m.entries,
	}).Info("Session ended")

}

type metrics struct {
	sessionStart    time.Time
	sessionID       int
	sessionDuration int
	remoteIP        net.IP
	localPort       int
	remotePort      int
	input           []byte
	inputTimes      []int64
	usernames       []string
	passwords       []string
	entries         []string
}

func logSessionTime(start time.Time, log *log.Entry) {
	log.WithField("session_time", time.Since(start)/1000000).Info("Connection closed")
}
