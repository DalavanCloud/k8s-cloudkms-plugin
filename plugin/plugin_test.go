/*
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package plugin

import (
	"log"
	"testing"

	k8spb "github.com/immutablet/k8s-cloudkms-plugin/v1beta1"

	"fmt"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/context"
)

const (
	keyURI           = "projects/cloud-kms-lab/locations/us-central1/keyRings/ring-01/cryptoKeys/key-01"
	pathToUnixSocket = "/tmp/test.socket"
)

// Logger allows t.Testing and b.Testing to be passed to a method that executes testing logic.
type Logger interface {
	Errorf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})
	Fatal(args ...interface{})
	Logf(format string, args ...interface{})
}

func TestEncryptDecrypt(t *testing.T) {
	plainText := []byte("secret")

	sut, err := New(keyURI, pathToUnixSocket, "")
	if err != nil {
		t.Fatalf("failed to instantiate plugin, %v", err)
	}

	encryptRequest := k8spb.EncryptRequest{Version: APIVersion, Plain: []byte(plainText)}
	encryptResponse, err := sut.Encrypt(context.Background(), &encryptRequest)

	if err != nil {
		t.Fatal(err)
	}

	decryptRequest := k8spb.DecryptRequest{Version: APIVersion, Cipher: []byte(encryptResponse.Cipher)}
	decryptResponse, err := sut.Decrypt(context.Background(), &decryptRequest)
	if err != nil {
		t.Error(err)
	}

	if string(decryptResponse.Plain) != string(plainText) {
		t.Fatalf("Expected secret, but got %s", string(decryptResponse.Plain))
	}
}

func TestDecryptionError(t *testing.T) {
	plainText := []byte("secret")

	sut, err := New(keyURI, pathToUnixSocket, "")
	if err != nil {
		t.Fatalf("failed to instantiate plugin, %v", err)
	}

	encryptRequest := k8spb.EncryptRequest{Version: APIVersion, Plain: []byte(plainText)}
	encryptResponse, err := sut.Encrypt(context.Background(), &encryptRequest)

	if err != nil {
		t.Fatal(err)
	}

	decryptRequest := k8spb.DecryptRequest{Version: APIVersion, Cipher: []byte(encryptResponse.Cipher[1:])}
	_, err = sut.Decrypt(context.Background(), &decryptRequest)
	if err == nil {
		t.Fatal(err)
	}
}

func TestRPC(t *testing.T) {
	plugin, client, err := setup()
	defer plugin.Stop()
	if err != nil {
		t.Fatalf("setup failed, %v", err)
	}

	runGRPCTest(t, client, []byte("secret"))
}

func BenchmarkRPC(b *testing.B) {
	b.StopTimer()
	plugin, client, err := setup()
	defer plugin.Stop()
	if err != nil {
		b.Fatalf("setup failed, %v", err)
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		runGRPCTest(b, client, []byte("secret"+strconv.Itoa(i)))
	}
	b.StopTimer()
	printMetrics(b)
}

func setup() (*Plugin, k8spb.KeyManagementServiceClient, error) {
	sut, err := New(keyURI, pathToUnixSocket, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to instantiate plugin, %v", err)
	}
	err = sut.SetupRPCServer()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start gRPC Server, %v", err)
	}

	go sut.Server.Serve(sut.Listener)

	connection, err := sut.NewUnixSocketConnection()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open unix socket, %v", err)
	}

	client := k8spb.NewKeyManagementServiceClient(connection)
	return sut, client, nil
}

func runGRPCTest(l Logger, client k8spb.KeyManagementServiceClient, plainText []byte) {
	encryptRequest := k8spb.EncryptRequest{Version: APIVersion, Plain: plainText}
	encryptResponse, err := client.Encrypt(context.Background(), &encryptRequest)
	if err != nil {
		l.Fatal(err)
	}

	decryptRequest := k8spb.DecryptRequest{Version: APIVersion, Cipher: []byte(encryptResponse.Cipher)}
	decryptResponse, err := client.Decrypt(context.Background(), &decryptRequest)
	if err != nil {
		l.Fatal(err)
	}

	if string(decryptResponse.Plain) != string(plainText) {
		l.Fatalf("Expected secret, but got %s", string(decryptResponse.Plain))
	}

	printMetrics(l)
}

func printMetrics(l Logger) error {
	metrics, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return fmt.Errorf("failed to gather metrics: %s", err)
	}

	metricsOfInterest := []string{
		"cloudkms_kms_client_operation_latency_microseconds",
		"go_memstats_alloc_bytes_total",
		"go_memstats_frees_total",
		"process_cpu_seconds_total",
	}

	for _, mf := range metrics {
		// l.Logf("%s", *mf.Name)
		if contains(metricsOfInterest, *mf.Name) {
			for _, metric := range mf.GetMetric() {
				l.Logf("%v", metric)
			}
		}
	}

	return nil
}

func ExampleEncrypt() {
	plainText := []byte("secret")

	plugin, err := New(keyURI, pathToUnixSocket, "")
	if err != nil {
		log.Fatalf("failed to instantiate plugin, %v", err)
	}

	encryptRequest := k8spb.EncryptRequest{Version: "v1beta1", Plain: []byte(plainText)}
	encryptResponse, err := plugin.Encrypt(context.Background(), &encryptRequest)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Cipher: %s", string(encryptResponse.Cipher))
}

func ExampleDecrypt() {
	cipher := "secret goes here"

	plugin, err := New(keyURI, pathToUnixSocket, "")
	if err != nil {
		log.Fatalf("failed to instantiate plugin, %v", err)
	}

	decryptRequest := k8spb.DecryptRequest{Version: "v1beta1", Cipher: []byte(cipher)}
	decryptResponse, err := plugin.Decrypt(context.Background(), &decryptRequest)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Plain: %s", string(decryptResponse.Plain))
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
