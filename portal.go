package main

import (
	"crypto/tls"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"

	"golang.org/x/crypto/acme/autocert"

	"github.com/bahusvel/TunnelBeast/auth"
	"github.com/bahusvel/TunnelBeast/boltdb"
	"github.com/bahusvel/TunnelBeast/config"
	"github.com/bahusvel/TunnelBeast/iptables"
)

type EntrySet map[iptables.NATEntry]interface{}

var (
	authProvider    auth.AuthProvider
	connectionTable = map[string]EntrySet{}
	portTable       = map[string]map[string]interface{}{}
	PORTAL          *template.Template
	LOGOUT          *template.Template
	conf            config.Configuration
)

func AddRoute(w http.ResponseWriter, r *http.Request) {
	log.Println("Api access", r.RemoteAddr)

	err := r.ParseForm()
	if err != nil {
		log.Println(err)
	}
	username := r.PostForm.Get("username")
	password := r.PostForm.Get("password")
	sourceip := r.PostForm.Get("sourceip")
	internalip := r.PostForm.Get("internalip")
	internalport := r.PostForm.Get("internalport")
	externalport := r.PostForm.Get("externalport")

	log.Println(username, password, internalip, internalport, externalport)
	if username == "" || password == "" || internalip == "" || internalport == "" || externalport == "" {
		w.Write([]byte("ERROR INPUT"))
		return
	}

	if sourceip == "" {
		sourceip = strings.Split(r.RemoteAddr, ":")[0]
	}

	entry := iptables.NATEntry{SourceIP: sourceip, DestinationIP: internalip, ExternalPort: externalport, InternalPort: internalport}

	if !authProvider.Authenticate(username, password) {
		w.Write([]byte("ERROR AUTH"))
		return
	}

	if !authProvider.CheckDestinationIP(internalip, username) {
		w.Write([]byte("ERROR ACCESS DENIED"))
		return
	}

	if entries, ok := connectionTable[username]; ok {
		if _, ok := entries[entry]; ok {
			w.Write([]byte("ERROR EXISTS"))
			return
		}
	} else {
		connectionTable[username] = EntrySet{}
	}

	availablePorts, ok := portTable[sourceip]

	if !ok {
		availablePorts = map[string]interface{}{}
		for _, port := range conf.Ports {
			availablePorts[port] = nil
		}
	}

	if _, ok := availablePorts[entry.ExternalPort]; !ok {
		w.Write([]byte("ERROR PORT OCCUPIED"))
		return
	}

	err = iptables.NewRoute(entry)
	if err != nil {
		w.Write([]byte(err.Error()))
		return
	}

	delete(availablePorts, entry.ExternalPort)

	portTable[sourceip] = availablePorts
	connectionTable[username][entry] = nil
	//w.Body.Close()
	w.Write([]byte("OK"))
}

func DeleteRoute(w http.ResponseWriter, r *http.Request) {
	log.Println("Delete request", r.RemoteAddr)

	err := r.ParseForm()
	if err != nil {
		log.Println(err)
	}
	username := r.PostForm.Get("username")
	password := r.PostForm.Get("password")
	sourceip := r.PostForm.Get("sourceip")
	internalip := r.PostForm.Get("internalip")
	internalport := r.PostForm.Get("internalport")
	externalport := r.PostForm.Get("externalport")

	entry := iptables.NATEntry{SourceIP: sourceip, DestinationIP: internalip, ExternalPort: externalport, InternalPort: internalport}

	log.Println(entry)

	if !authProvider.Authenticate(username, password) {
		w.Write([]byte("ERROR AUTH"))
		return
	}

	entries, ok := connectionTable[username]
	if !ok {
		w.Write([]byte("ERROR NOT EXIST"))
		return
	}

	if _, ok = entries[entry]; !ok {
		w.Write([]byte("ERROR NOT EXIST"))
		return
	}

	err = iptables.DeleteRoute(entry)
	if err != nil {
		w.Write([]byte(err.Error()))
		return
	}

	portTable[sourceip][entry.ExternalPort] = nil
	delete(connectionTable[username], entry)
	w.Write([]byte("OK"))
}

func PortalEntryHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Portal access", r.RemoteAddr)

	if r.URL.Path == "/" {
		asset, _ := Asset("html/index.html")
		w.Write(asset)
		return
	}

	asset, err := Asset("html" + r.URL.Path)
	if err != nil {
		w.Write([]byte("404"))
		return
	}
	w.Write(asset)
}

func ListRoutes(w http.ResponseWriter, r *http.Request) {
	log.Println("List access", r.RemoteAddr)
	w.Header().Set("Cache-Control", "no-cache")

	err := r.ParseForm()
	if err != nil {
		log.Println(err)
	}

	username := r.PostForm.Get("username")
	password := r.PostForm.Get("password")

	if !authProvider.Authenticate(username, password) {
		w.Write([]byte("ERROR AUTH"))
		return
	}

	entries := connectionTable[username]
	log.Println(len(entries))
	keys := make([]iptables.NATEntry, len(entries))
	i := 0
	for k := range entries {
		keys[i] = k
		i++
	}

	data, err := json.Marshal(keys)
	if err != nil {
		log.Fatal(err)
	}
	w.Write(data)
}

func ListPorts(w http.ResponseWriter, r *http.Request) {
	log.Println("Port access", r.RemoteAddr)
	w.Header().Set("Cache-Control", "no-cache")

	sourceip := strings.Split(r.RemoteAddr, ":")[0]

	var keys []string
	entries, ok := portTable[sourceip]
	if !ok {
		keys = conf.Ports
	} else {
		keys = make([]string, len(entries))
		i := 0
		for k := range entries {
			keys[i] = k
			i++
		}
	}

	log.Println(keys)

	data, err := json.Marshal(keys)
	if err != nil {
		log.Fatal(err)
	}
	w.Write(data)
}

func Authenticate(w http.ResponseWriter, r *http.Request) {
	log.Println("List access", r.RemoteAddr)

	err := r.ParseForm()
	if err != nil {
		log.Println(err)
	}

	username := r.PostForm.Get("username")
	password := r.PostForm.Get("password")

	if !authProvider.Authenticate(username, password) {
		w.Write([]byte("ERROR AUTH"))
		return
	}
	w.Write([]byte("OK"))
}

func AddRecord(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		log.Println(err)
	}

	username := r.PostForm.Get("username")
	password := r.PostForm.Get("password")
	internalip := r.PostForm.Get("internalip")
	internalport := r.PostForm.Get("internalport")
	externalport := r.PostForm.Get("externalport")
	recordname := r.PostForm.Get("recordname")

	if username == "" || password == "" || internalip == "" || internalport == "" || externalport == "" {
		w.Write([]byte("ERROR INPUT"))
		return
	}

	if !authProvider.Authenticate(username, password) {
		w.Write([]byte("ERROR AUTH"))
		return
	}

	key := username + "/favorite/" + recordname
	value := boltdb.RecordValue{DestinationIP: internalip, ExternalPort: externalport, InternalPort: internalport, RecordName: recordname}

	err = boltdb.AddRecord(key, value)
	if err != nil {
		log.Println(err)
		w.Write([]byte(err.Error()))
		return
	}

	log.Println("favorite route added: ", username, recordname, externalport, internalip, internalport)
	w.Write([]byte("OK"))
}

func DeleteRecord(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		log.Println(err)
	}

	username := r.PostForm.Get("username")
	password := r.PostForm.Get("password")
	internalip := r.PostForm.Get("internalip")
	internalport := r.PostForm.Get("internalport")
	externalport := r.PostForm.Get("externalport")
	recordname := r.PostForm.Get("recordname")

	if username == "" || password == "" || internalip == "" || internalport == "" || externalport == "" || recordname == "" {
		w.Write([]byte("ERROR INPUT"))
		return
	}

	if !authProvider.Authenticate(username, password) {
		w.Write([]byte("ERROR AUTH"))
		return
	}

	key := username + "/favorite/" + recordname

	err = boltdb.DeleteRecord(key)
	if err != nil {
		log.Println(err)
		w.Write([]byte(err.Error()))
		return
	}

	log.Println("favorite route deleted: ", username, recordname, externalport, internalip, internalport)
	w.Write([]byte("OK"))
}

func ListRecords(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		log.Println(err)
	}

	username := r.PostForm.Get("username")
	password := r.PostForm.Get("password")

	if !authProvider.Authenticate(username, password) {
		w.Write([]byte("ERROR AUTH"))
		return
	}

	values := make([]boltdb.RecordValue, 0)
	values, err = boltdb.ListRecords(username)
	if err != nil {
		log.Fatal(err)
		w.Write([]byte("ERROR RECORD"))
	}

	data, err := json.Marshal(values)
	if err != nil {
		log.Fatal(err)
	}

	w.Write(data)
}

func redirectTLS(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://"+r.Host+r.RequestURI, http.StatusMovedPermanently)
}

func main() {
	if len(os.Args) != 2 {
		log.Fatalln("Usage: TunnelBeast /path/to/config.yml")
	}

	config.LoadConfig(os.Args[1], &conf)
	log.Printf("%+v\n", conf)

	authProvider = conf.AuthProvider
	authProvider.Init()

	err := iptables.Init(conf.ListenDev)
	if err != nil {
		log.Println("Error initializing iptables", err)
		return
	}

	err = boltdb.Init(conf.DBpath)
	if err != nil {
		log.Println("Error open boltdb", err)
	}

	certManager := autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(conf.Domainname),
		Cache:      autocert.DirCache("certs"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", PortalEntryHandler)
	mux.HandleFunc("/delete", DeleteRoute)
	mux.HandleFunc("/add", AddRoute)
	mux.HandleFunc("/ports", ListPorts)
	mux.HandleFunc("/auth", Authenticate)
	mux.HandleFunc("/list", ListRoutes)
	mux.HandleFunc("/addRecord", AddRecord)
	mux.HandleFunc("/deleteRecord", DeleteRecord)
	mux.HandleFunc("/listRecords", ListRecords)

	port80 := &http.Server{}

	switch conf.Https {
	case "none":
		port80 = &http.Server{Addr: ":80", Handler: mux}
	case "manual":
		port80 = &http.Server{Addr: ":80", Handler: http.HandlerFunc(redirectTLS)}
		port443 := &http.Server{Addr: ":443", Handler: mux}
		go func() { err = port443.ListenAndServeTLS(conf.Path, conf.Path[:len(conf.Path)-4]+".key") }()
	case "letsEncrypt":
		port80 = &http.Server{Addr: ":80", Handler: http.HandlerFunc(redirectTLS)}
		port443 := &http.Server{Addr: ":443", Handler: mux, TLSConfig: &tls.Config{GetCertificate: certManager.GetCertificate}}
		go func() { err = port443.ListenAndServeTLS("", "") }() //key and cert are coming from Let's tEncrypti
	default:
		log.Println("Error https configure")
		return
	}

	if err != nil {
		log.Println(err)
	}

	errInternal := port80.ListenAndServe()
	if errInternal != nil {
		log.Println(errInternal)
	}
}
