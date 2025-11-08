// ============================================
// File: internal/media/m3u8.go
package media

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
)

// CheckM3u8 connects to device server to get enhanced m3u8 URL
func CheckM3u8(adamID string, itemType string, getFromDevice bool, port string) (string, error) {
	var enhancedHls string

	if !getFromDevice {
		return "none", nil
	}

	conn, err := net.Dial("tcp", port)
	if err != nil {
		fmt.Println("Error connecting to device:", err)
		return "none", err
	}
	defer conn.Close()

	if itemType == "song" {
		fmt.Println("Connected to device")
	}

	adamIDBuffer := []byte(adamID)
	lengthBuffer := []byte{byte(len(adamIDBuffer))}

	_, err = conn.Write(lengthBuffer)
	if err != nil {
		fmt.Println("Error writing length to device:", err)
		return "none", err
	}

	_, err = conn.Write(adamIDBuffer)
	if err != nil {
		fmt.Println("Error writing adamID to device:", err)
		return "none", err
	}

	response, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		fmt.Println("Error reading response from device:", err)
		return "none", err
	}

	response = bytes.TrimSpace(response)
	if len(response) > 0 {
		if itemType == "song" {
			fmt.Println("Received URL:", string(response))
		}
		enhancedHls = string(response)
	} else {
		fmt.Println("Received an empty response")
	}

	return enhancedHls, nil
}
