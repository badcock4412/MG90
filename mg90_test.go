package MG90

import (
	"testing"
	"time"
	"fmt"
)

const (
	MG90IP = "172.22.0.1"
	MG90USER = "admin"
	MG90PASS = "admin"
)

func TestMG90Start(t *testing.T) {
	MG, err := NewMG90("172.22.0.1")
	
	if err != nil {
		t.Error(err)
	}
	
	MG.Close()
	
}


func TestMG90AuthRequired(t *testing.T) {
	MG, err := NewMG90("172.22.0.1")
	if err != nil {
		t.Error(err)
	}
	defer MG.Close()
	
	if MG.testAuth() {
		t.Error("testAuth believes non-auth session is auth")
	}
}

func TestMG90AuthSuccess(t *testing.T) {
	MG, err := NewMG90("172.22.0.1")
	if err != nil {
		t.Error(err)
	}
	defer MG.Close()
	
	MG.Credentials.Username = "admin"
	MG.Credentials.Password = "admin"
	
	MG.prep(res["Main"])
	
	if ! MG.testAuth() {
		t.Error("test auth believes auth session is non-auth")
	}
	
}


func TestMG90AuthFailure(t *testing.T) {
	MG, err := NewMG90("172.22.0.1")
	if err != nil {
		t.Error(err)
	}
	defer MG.Close()
	
	MG.Credentials.Username = "admin"
	MG.Credentials.Password = "admi"
	
	MG.prep(res["Main"])
	
	if MG.testAuth() {
		t.Error("test auth believes bad password session is auth")
	}	
}

func TestBeaconService(t *testing.T) {
	MG, err := NewMG90("172.22.0.1")
	if err != nil {
		t.Error(err)
	}
	defer MG.Close()
	
	go MG.ListenBeacon(15000)
	
	c := time.NewTimer( 20 * time.Second )
	select {
		case e := <-MG.Events.GPIO:
			fmt.Println(e)
		case <-c.C:
			t.Error("Timeout waiting for beacon message")
	}
	
}