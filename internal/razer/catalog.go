package razer

import (
	"embed"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// The device table, generated from openrazer by scripts/gendevices.py
// (laptops and headsets are left out).
//
//go:embed data/devices.json
var dataFS embed.FS

type seqStep struct {
	TransactionID  string `json:"transactionId"`
	FixedArgument0 string `json:"fixedArgument0"`
}

type rawDevice struct {
	ProductID    string               `json:"productId"`
	DisplayName  string               `json:"displayName"`
	Category     string               `json:"category"`
	Methods      []string             `json:"methods"` // openrazer daemon methods
	MaximumDPI   int                  `json:"maximumDpi"`
	PollRates    []int                `json:"pollRates"`
	Transactions map[string]string    `json:"transactions"`
	Prefixes     map[string][]string  `json:"builderArgumentPrefixes"`
	Sequences    map[string][]seqStep `json:"commandSequences"`
}

// Model is one product and the openrazer commands it answers.
type Model struct {
	PID       uint16
	Name      string
	Category  string // mouse, keyboard, mouse-mat, accessory, monitor
	MaxDPI    int
	PollRates []int

	tx        map[string]byte // builder name (without razer_chroma_) -> transaction id
	prefixes  map[string][][]byte
	pollSeq   []seqStep
	methods   map[string]bool // openrazer daemon methods, used to find lighting zones
	receivers bool            // HyperPolling receiver with three indicator LEDs
}

// Models is the device table, sorted by product id.
var Models = loadModels()

func loadModels() []Model {
	var out []Model
	seen := map[uint16]bool{}
	{
		b, err := dataFS.ReadFile("data/devices.json")
		if err != nil {
			panic(err)
		}
		var file struct {
			Devices []rawDevice `json:"devices"`
		}
		if err := json.Unmarshal(b, &file); err != nil {
			panic("razer: bad device table: " + err.Error())
		}
		for _, d := range file.Devices {
			pid, err := strconv.ParseUint(d.ProductID, 16, 16)
			if err != nil || seen[uint16(pid)] {
				continue
			}
			seen[uint16(pid)] = true
			m := Model{PID: uint16(pid), Name: d.DisplayName, Category: d.Category, MaxDPI: d.MaximumDPI,
				PollRates: d.PollRates, tx: map[string]byte{}, prefixes: map[string][][]byte{}, methods: map[string]bool{}}
			for k, v := range d.Transactions {
				if t, err := strconv.ParseUint(v, 16, 8); err == nil {
					m.tx[strings.TrimPrefix(k, "razer_chroma_")] = byte(t)
				}
			}
			for k, list := range d.Prefixes {
				for _, p := range list {
					if b, err := hex.DecodeString(p); err == nil {
						k := strings.TrimPrefix(k, "razer_chroma_")
						m.prefixes[k] = append(m.prefixes[k], b)
					}
				}
			}
			if s := d.Sequences["razer_chroma_misc_set_polling_rate2"]; len(s) > 0 {
				m.pollSeq = s
				m.tx["misc_set_polling_rate2"] = parseTx(s[0].TransactionID)
			}
			for _, name := range d.Methods {
				m.methods[name] = true
			}
			m.receivers = m.methods["set_hyperpolling_wireless_dongle_multi_indicator_led_modes"]
			if !m.has("standard_get_firmware_version") {
				// Every device answers it; the driver only lists ids it overrides.
				m.tx["standard_get_firmware_version"] = 0xFF
			}
			m.fillLightingGaps()
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

func parseTx(s string) byte {
	t, _ := strconv.ParseUint(s, 16, 8)
	return byte(t)
}

func findModel(pid uint16) *Model {
	i := sort.Search(len(Models), func(i int) bool { return Models[i].PID >= pid })
	if i < len(Models) && Models[i].PID == pid {
		return &Models[i]
	}
	return nil
}

func (m *Model) has(name string) bool { _, ok := m.tx[name]; return ok }

// hasPrefix reports whether any builder name starts with p.
func (m *Model) hasPrefix(p string) bool {
	for k := range m.tx {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}
