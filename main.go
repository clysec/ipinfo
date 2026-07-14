package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strings"

	"github.com/ip2location/ip2location-go/v9"
	"github.com/scheiblingco/gofn/bytetools"

	_ "embed"
)

//go:embed index.html
var index []byte

//go:embed paper.jpg
var paperImage []byte

//go:embed ipinfo.css
var css []byte

//go:embed lines.png
var linesImage []byte

//go:embed geoip.bin.gz
var geoipdb []byte

//go:embed asnip.bin.gz
var asnipdb []byte

//go:embed mapping.json
var ipw4mapping []byte

type KvItem struct {
	Key     string
	Value   string
	ValueId string
}

func UnzipGeoip() (*ip2location.DB, *ip2location.DB, error) {
	r, err := gzip.NewReader(bytes.NewReader(geoipdb))
	if err != nil {
		return nil, nil, err
	}

	defer r.Close()

	geoipr, err := bytetools.NewSeekableBufferFromReader(r)
	if err != nil {
		return nil, nil, err
	}

	r2, err := gzip.NewReader(bytes.NewReader(asnipdb))
	if err != nil {
		return nil, nil, err
	}
	defer r2.Close()

	asnipr, err := bytetools.NewSeekableBufferFromReader(r2)
	if err != nil {
		return nil, nil, err
	}

	geoipdb, err := ip2location.OpenDBWithReader(geoipr)
	if err != nil {
		return nil, nil, err
	}

	asnipdb, err := ip2location.OpenDBWithReader(asnipr)
	if err != nil {
		return nil, nil, err
	}

	return geoipdb, asnipdb, nil
}

type DisplayData struct {
	Attributes map[int]KvItem
	IPW4       string
}

func (dd *DisplayData) SetAttribute(key string, value string, valueId ...string) {
	if dd.Attributes == nil {
		dd.Attributes = make(map[int]KvItem)
	}

	index := len(dd.Attributes)
	if len(valueId) > 0 {
		dd.Attributes[index] = KvItem{Key: key, Value: value, ValueId: valueId[0]}
		return
	}

	dd.Attributes[index] = KvItem{Key: key, Value: value}
}

type IPW4 struct {
	Mapping map[string]string
}

func GetIpw4() (IPW4, error) {
	ipw4 := IPW4{}

	err := json.Unmarshal(ipw4mapping, &ipw4.Mapping)
	if err != nil {
		return IPW4{}, err
	}

	return ipw4, nil
}

func (i *IPW4) GetMapped(ipv4string string) (string, bool) {
	if i.Mapping == nil {
		return "", false
	}

	parts := strings.Split(ipv4string, ".")
	if len(parts) != 4 {
		return "", false
	}

	full := []string{}

	if mapped, ok := i.Mapping[parts[0]]; ok {
		full = append(full, mapped)
	}

	if mapped, ok := i.Mapping[parts[1]]; ok {
		full = append(full, mapped)
	}

	if mapped, ok := i.Mapping[parts[2]]; ok {
		full = append(full, mapped)
	}

	if mapped, ok := i.Mapping[parts[3]]; ok {
		full = append(full, mapped)
	}

	return strings.Join(full, "-") + ".ipw4.com", true
}

func main() {
	geoip, asnip, err := UnzipGeoip()
	if err != nil {
		panic(err)
	}

	defer geoip.Close()
	defer asnip.Close()

	ipw4, err := GetIpw4()
	if err != nil {
		panic(err)
	}

	indexTemplate := template.Must(template.New("index").Parse(string(index)))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "plain" || r.Header.Get("X-Output") == "iponly" || r.Header.Get("X-Output") == "plain" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			if strings.HasSuffix(r.Header.Get("Origin"), ".dnsn.eu") {
				w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
			} else {
				w.Header().Set("Access-Control-Allow-Origin", "https://.*\\.dnsn.eu")
			}
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "X-Requested-With, X-Output")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == "OPTIONS" {
			fmt.Println("OPTIONS request")
			w.WriteHeader(http.StatusOK)
			return
		}

		// If url is healthz return 200 OK
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
			return
		}

		// If URL is /paper.jpg serve the image
		if r.URL.Path == "/paper.jpg" {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write(paperImage)
			return
		}

		// If URL is /lines.png serve the image
		if r.URL.Path == "/lines.png" {
			w.Header().Set("Content-Type", "image/png")
			w.Write(linesImage)
			return
		}

		if r.URL.Path == "/ipinfo.css" {
			w.Header().Set("Content-Type", "text/css")
			w.Write(css)
			return
		}

		ipaddr := r.RemoteAddr

		if r.Header.Get("X-Forwarded-For") != "" {
			ipaddr = r.Header.Get("X-Forwarded-For")
		}

		parsedIp, _, err := net.SplitHostPort(ipaddr)
		if err == nil {
			ipaddr = parsedIp
		}

		fmt.Println("Request from IP:", ipaddr)

		if r.URL.Path == "plain" || r.Header.Get("X-Output") == "iponly" || r.Header.Get("X-Output") == "plain" {
			fmt.Println("Serving plain IP response")
			// Set CORS headers
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(ipaddr))
			return
		}
		fmt.Println("Serving HTML response")

		data := DisplayData{}

		if strings.Count(ipaddr, ".") == 3 {
			data.SetAttribute("IPv4", ipaddr)
			data.SetAttribute("IPv6", "Checking...", "ipv6-status")
		} else {
			data.SetAttribute("IPv4", "Checking...", "ipv4-status")
			data.SetAttribute("IPv6", ipaddr)
		}

		asndata, err := asnip.Get_asn(ipaddr)
		if err != nil {
			data.SetAttribute("ASN", "N/A")
		} else {
			data.SetAttribute("ASN", asndata.Asn)
		}

		geoipdata, err := geoip.Get_all(ipaddr)
		if err != nil {
			data.SetAttribute("Country", "N/A")
			data.SetAttribute("Region", "N/A")
			data.SetAttribute("City", "N/A")
			data.SetAttribute("ISP", "N/A")
		} else {
			data.SetAttribute("Country", geoipdata.Country_long)
			data.SetAttribute("Region", geoipdata.Region)
			data.SetAttribute("City", geoipdata.City)
		}

		if strings.Count(ipaddr, ".") == 3 {
			if mapped, ok := ipw4.GetMapped(ipaddr); ok {
				data.IPW4 = strings.ToLower(mapped)
			}
		}

		w.Header().Set("Content-Type", "text/html")
		indexTemplate.Execute(w, data)
	})

	fmt.Println("Listening on :8989")
	err = http.ListenAndServe(":8989", nil)
	if err != nil {
		panic(err)
	}
}
