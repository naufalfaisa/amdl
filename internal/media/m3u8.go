// ============================================
// File: internal/media/m3u8.go
package media

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
)

const (
	itemTypeSong = "song"
	noneResponse = "none"
)

// CheckM3u8 connects to device server to get enhanced m3u8 URL
func CheckM3u8(adamID string, itemType string, getFromDevice bool, port string) (string, error) {
	if !getFromDevice {
		return noneResponse, nil
	}

	conn, err := connectToDevice(port)
	if err != nil {
		return noneResponse, err
	}
	defer conn.Close()

	if itemType == itemTypeSong {
		fmt.Println("Connected to device")
	}

	if err := sendAdamID(conn, adamID); err != nil {
		return noneResponse, err
	}

	enhancedHls, err := receiveResponse(conn, itemType)
	if err != nil {
		return noneResponse, err
	}

	return enhancedHls, nil
}

// connectToDevice establishes TCP connection to device server
func connectToDevice(port string) (net.Conn, error) {
	conn, err := net.Dial("tcp", port)
	if err != nil {
		return nil, fmt.Errorf("connect to device: %w", err)
	}
	return conn, nil
}

// sendAdamID sends adamID to device with length prefix
func sendAdamID(conn net.Conn, adamID string) error {
	adamIDBytes := []byte(adamID)
	lengthByte := []byte{byte(len(adamIDBytes))}

	// Send length prefix
	if _, err := conn.Write(lengthByte); err != nil {
		return fmt.Errorf("write length to device: %w", err)
	}

	// Send adamID
	if _, err := conn.Write(adamIDBytes); err != nil {
		return fmt.Errorf("write adamID to device: %w", err)
	}

	return nil
}

// receiveResponse reads and parses response from device
func receiveResponse(conn net.Conn, itemType string) (string, error) {
	response, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return "", fmt.Errorf("read response from device: %w", err)
	}

	response = bytes.TrimSpace(response)

	if len(response) == 0 {
		return "", fmt.Errorf("received empty response from device")
	}

	enhancedHls := string(response)

	if itemType == itemTypeSong {
		fmt.Println("Received URL:", enhancedHls)
	}

	return enhancedHls, nil
}
