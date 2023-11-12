package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	localHeight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "bridge_local_height",
		Help: "Local height of the Celestia node",
	})

	networkHeight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "bridge_network_height",
		Help: "Network height of the Celestia node",
	})
)

func init() {
	prometheus.MustRegister(localHeight)
	prometheus.MustRegister(networkHeight)
}

func main() {
	listenPort := flag.String("listen.port", "8380", "port to listen on")
	endpoint := flag.String("endpoint", "http://localhost:26658", "endpoint to connect to")
	p2pNetwork := flag.String("p2p.network", "blockspacerace", "network to use")
	flag.Parse()

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		promhttp.Handler().ServeHTTP(w, r)
	})

	go func() {
		client := &http.Client{}
		authToken := getAuthToken(*p2pNetwork)
		for {
			updateMetrics(client, authToken, *endpoint)
			time.Sleep(5 * time.Second)
		}
	}()

	log.Printf("Celestia Bridge Exporter started on port %s\n", *listenPort)
	log.Fatal(http.ListenAndServe(":"+*listenPort, nil))
}

func updateMetrics(client *http.Client, authToken, endpoint string) {
	local, network, err := getHeights(client, authToken, endpoint)
	if err != nil {
		log.Printf("Error getting heights: %v\n", err)
		return
	}

	localHeight.Set(float64(local))
	networkHeight.Set(float64(network))
}

func getHeights(client *http.Client, authToken, endpoint string) (int, int, error) {
	local := getHeight(client, authToken, "header.LocalHead", endpoint)
	network := getHeight(client, authToken, "header.NetworkHead", endpoint)

	return local, network, nil
}

func getHeight(client *http.Client, authToken, method, endpoint string) int {
	reqData := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  []interface{}{},
	}
	reqBytes, _ := json.Marshal(reqData)

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(reqBytes))
	if err != nil {
		log.Printf("Error creating request: %v\n", err)
		return 0
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", authToken))

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error executing request: %v\n", err)
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Non-OK HTTP status: %v\n", resp.Status)
		return 0
	}

	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response body: %v\n", err)
		return 0
	}

	var respData map[string]interface{}
	if err := json.Unmarshal(respBytes, &respData); err != nil {
		log.Printf("Error unmarshaling response: %v\n", err)
		return 0
	}

	result, ok := respData["result"].(map[string]interface{})
	if !ok {
		log.Println("Error: result is not a map")
		return 0
	}

	header, ok := result["header"].(map[string]interface{})
	if !ok {
		log.Println("Error: header is not a map")
		return 0
	}

	heightStr, ok := header["height"].(string)
	if !ok {
		log.Println("Error: height is not a string")
		return 0
	}

	height, err := strconv.Atoi(heightStr)
	if err != nil {
		log.Printf("Error converting height to int: %v\n", err)
		return 0
	}
	return height
}

func getAuthToken(p2pNetwork string) string {
	out, err := exec.Command("celestia", "bridge", "auth", "admin", "--p2p.network", p2pNetwork).CombinedOutput()
	if err != nil {
		log.Printf("Error getting auth token: %v, output: %s\n", err, string(out))
		return ""
	}

	return strings.TrimSpace(string(out))
}
