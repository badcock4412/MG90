package MG90

import (
	"net/http"
	"net/http/cookiejar"
	"golang.org/x/net/publicsuffix"
	"github.com/sparrc/go-ping"
	"time"
	"io"
	"net/url"
	"golang.org/x/net/html"
	"errors"
	"fmt"
	"strings"
	"strconv"
	"io/ioutil"
    "net"
	"bytes"
	"encoding/json"
)

type MG90 struct {
	// IPAddress of the MG90
	IPAddress string
	
	// Credentials to use
	Credentials struct {
		Username string
		Password string
	}
	
	Events struct {
		GPIO chan GPIOEvent
		LostConnection chan error
	}
	
	id string
	
	httpclient *http.Client
	beaconclient *net.UDPConn
	monitor *time.Ticker
	
}

type GPIOEvent struct {
	Channel int
	OldValue *int
	NewValue int
}

type mg90resource struct {
	Port int
	AuthRequired bool
	URI string
}

var (
	res = map[string]mg90resource {
		"EZ": { 80, false, "/MG-LCI/easyaccess.html" },
		"Main": { 80, true, "/MG-LCI/main.html" },
		"AMMConfig" : { 80, true, "/MG-LCI/wan/vpn/managementtunnel.html?editId=Management+Tunnel" },
	}
)

func NewMG90(address string) (*MG90, error) {

	m := &MG90{ IPAddress:address }

	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, err
	}
	
	m.httpclient = &http.Client{
		Timeout: time.Second * 6,
		Jar: jar,
	}
	
	if err := m.fetchId(); err != nil {
		return nil, err
	}
	
	m.Events.GPIO = make(chan GPIOEvent,4)
	m.Events.LostConnection = make(chan error)

	return m, nil
}

func (m *MG90) SetTimeout( d time.Duration ) {
	m.httpclient.Timeout = d
}

func (m *MG90) StartBeaconListener( port int ) (error) {
	panic("Not Implemented")
	return nil
}

func (m *MG90) Close() {
	if m.beaconclient != nil {
		m.beaconclient.Close()
		m.beaconclient = nil
	}
	if m.monitor != nil {
		m.monitor.Stop()
		m.monitor = nil
	}
	if m.httpclient != nil {
	
	}
	close(m.Events.GPIO)
	close(m.Events.LostConnection)
}

func (m *MG90) GetId() (string) {
	return m.id
}

func (m *MG90) fetchId () (error) {

	var getIDFromPage = func (page io.Reader) (string, error) {
		z := html.NewTokenizer(page)
		isHeader := false
		for {
			tt := z.Next()
			
			switch {
			case tt == html.ErrorToken:
				return "", errors.New("Did not find MG-90 ID in page")
			case tt == html.StartTagToken:
				t := z.Token()
				
				if t.Data == "h1" {
					for _, a := range t.Attr {
						isHeader = a.Key == "align" && a.Val == "center"
					}
				}
			case tt == html.TextToken:
				if isHeader {
					return z.Token().Data, nil
				}
			}	
		}
	}
	
	url, err := m.prep(res["EZ"])
	if err != nil {
		return err
	}
	
	resp, err := m.httpclient.Get(url)
	if err != nil {
		return errors.New(fmt.Sprintf("Could not reach MG-90 at %s",m.IPAddress))
	}
	
	// Parse the webpage
	id, err := getIDFromPage(resp.Body)
	resp.Body.Close()
	if err != nil {
		return errors.New(fmt.Sprintf("Could not parse MG-90 ID from %s",m.IPAddress))
	}
	
	m.id = id

	return nil
}

func (m *MG90) testAuth() (bool) {
	
	resp, err := m.httpclient.Get("http://" + m.IPAddress + "/MG-LCI/main.html")
	if err != nil {
		return false
	}
	io.Copy(ioutil.Discard, resp.Body)
	resp.Body.Close()
	
	return ! ( strings.Contains("/login.html",resp.Request.URL.String()) || resp.StatusCode == 404 )
}

func (m *MG90) prep ( r mg90resource ) (string, error) {
	
	if r.AuthRequired && ! m.testAuth() {
		lurls := "login.html"
		if r.Port == 80 {
			lurls = "MG-LCI/" + lurls
		} 
		lurl, err := url.Parse("http://" + m.IPAddress + ":" + strconv.Itoa(r.Port) + "/" + lurls)
		if err != nil {
			return "", errors.New("Could not build path from given MG-90 address.")
		}
		
		data := url.Values{}
		data.Add("name", m.Credentials.Username)
		data.Add("password", m.Credentials.Password)

		resp, err := m.httpclient.PostForm(lurl.String(), data)
		if err != nil {
			return "", err
		}
		
		if resp.StatusCode != 200 {
			return "", errors.New(fmt.Sprintf("Call to %s resulted in %s\n",resp.Request.URL.String(), resp.Status))
		}
		
		io.Copy(ioutil.Discard, resp.Body)
		resp.Body.Close()
		
	}
	
	url, err := url.Parse("http://" + m.IPAddress + ":" + strconv.Itoa(r.Port) + r.URI)
	if err != nil {
		panic(err)
	}
	return url.String(), nil
}

// had to add this in /etc/sysctl.conf on Ubuntu 16.04
// for this to work:
// net.ipv4.ping_group_range = 0   2147483647
func (m *MG90) PingMonitor() {
	timeoutInterval := 5 * time.Second 
	pingInterval := 1 * time.Second
	
	pinger, err := ping.NewPinger(m.IPAddress)
	if err != nil {
		panic(err)
	}
	
	pinger.Interval = pingInterval
	pinger.SetPrivileged(PINGPRIVELEGE)
	
	toc := time.NewTimer(timeoutInterval)
	
	pinger.OnRecv = func(pkt *ping.Packet) {
		toc.Reset(timeoutInterval)
	}

	go pinger.Run()
	defer pinger.Stop()
	
	m.Events.LostConnection <- errors.New(fmt.Sprintf("MG-90 lost connection at %+v",<-toc.C))
	
}

func (m *MG90) StartMonitor(period time.Duration) {
	m.monitor = time.NewTicker(period)
	for range m.monitor.C {
		oldId := m.id
		err := m.fetchId()
		
		if err != nil {
			m.Events.LostConnection <- err
			break
		} else if oldId != m.id {
			m.Events.LostConnection <- errors.New(fmt.Sprintf("ID changed from %s to %s",oldId,m.id))
			break
		}
		
	}
}

func (m *MG90) ListenBeacon (port int) {

    //Resolving address
    udpAddr, err := net.ResolveUDPAddr("udp4", "0.0.0.0:" + strconv.Itoa(port))

    if err != nil {
        panic(err)
    }

    // Build listening connections
    m.beaconclient, err = net.ListenUDP("udp", udpAddr)
	
    if err != nil {
        panic(err)
    }

	var gpioStat [4]*int

    // Interacting with one client at a time
    for {

        // Receiving a message
        recvBuff := make([]byte, 15000)
		recvBuff2 := recvBuff
        rmLen, _, err := m.beaconclient.ReadFromUDP(recvBuff)
		
        if err != nil {
            break
        }
		
		if bytes.HasPrefix(recvBuff, []byte("GPIO:")) {
			recvBuff2 = recvBuff[5:rmLen]
		} else {
			recvBuff2 = recvBuff[:rmLen]
		}

		var dat map[string]interface{}

		if err := json.Unmarshal(recvBuff2, &dat); err != nil {
			panic(err)
		}
		
		gpios := dat["gpInputStates"].([]interface{})
		for i := 0; i < 4; i++ {
			newValue := int(gpios[i].(float64))
			if gpioStat[i] == nil  {
				select {
					case m.Events.GPIO <- GPIOEvent{ Channel: i+1, OldValue: nil, NewValue: newValue }:
					default:
				}
			} else {
				oldValue := *gpioStat[i]
				if oldValue != newValue {
					select {
						case m.Events.GPIO <- GPIOEvent{ Channel: i+1, OldValue: &oldValue, NewValue: newValue }:
						default:
					}
				}
			}
			gpioStat[i] = &newValue
		}
	}
}

func (m *MG90) FixAMMConnection() (error) {

	vpnURL, err := m.prep(res["AMMConfig"])
	if err != nil {
		return err
	}

	data := url.Values{}

	data.Add("formAction", "save")
	data.Add("automaticGatewayManager1", "true")
	data.Add("_automaticGatewayManager1", "1")
	data.Add("automaticGatewayManager2", "true")
	data.Add("_automaticGatewayManager2", "1")
	data.Add("udpPorts[0].selected","true")
	data.Add("_udpPorts[0].selected","1")
	data.Add("_udpPorts[1].selected","1")
	data.Add("_udpPorts[2].selected","1")
	data.Add("_udpPorts[3].selected","1")
	data.Add("autoMonitor","true")
	data.Add("_autoMonitor","1")


	resp, err := m.httpclient.Get(vpnURL)
	if err != nil {
		return err
	}
	
	io.Copy(ioutil.Discard, resp.Body)
	resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return errors.New(fmt.Sprintf("GET of VPN page returned %s",resp.Status))
	}
	
	resp2, err := m.httpclient.PostForm(vpnURL,data)
	
	if err != nil {
		return err
	}
	
	io.Copy(ioutil.Discard, resp2.Body)
	resp2.Body.Close()
	
	if resp2.StatusCode != 200 {
		return errors.New(fmt.Sprintf("POST of VPN page returned %s",resp2.Status))
	}
	
	return nil
}


