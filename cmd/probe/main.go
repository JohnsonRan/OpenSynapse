// probe prints the state of connected devices without changing anything.
package main

import (
	"fmt"
	"log"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
	"github.com/JohnsonRan/OpenSynapse/internal/walkplay"
)

func main() {
	devs, err := razer.Scan()
	if err != nil {
		log.Fatal(err)
	}
	dongles, _ := walkplay.Scan()
	if len(devs)+len(dongles) == 0 {
		fmt.Println("no supported device found")
	}
	for _, d := range dongles {
		fmt.Printf("%s %s\n", d.ID, d.Name)
		show := func(name string, v any, err error) { fmt.Printf("  %-12s %v %v\n", name, v, errStr(err)) }
		v, e := d.Version()
		show("firmware", v, e)
		eq, e := d.EQ()
		show("eq", fmt.Sprintf("%+v", eq), e)
		f, e := d.DACFilter()
		show("dacfilter", f, e)
		g, e := d.HighGain()
		show("highgain", g, e)
		w, e := d.WorkMode()
		show("workmode", w, e)
		m, e := d.MicGain()
		show("micgain", m, e)
		d.Close()
	}
	for _, d := range devs {
		fmt.Printf("%s %s (%d collections)\n", d.ID, d.Name, razer.Candidates(d))
		show := func(name string, v any, err error) { fmt.Printf("  %-12s %v %v\n", name, v, errStr(err)) }
		v1, e := d.DPI()
		show("dpi", v1, e)
		s, a, e := d.DPIStages()
		show("stages", fmt.Sprint(s, " active=", a), e)
		p, e := d.PollRate()
		show("poll", p, e)
		b, e := d.Battery()
		show("battery", b, e)
		c, e := d.Charging()
		show("charging", c, e)
		i, e := d.IdleTime()
		show("idle", i, e)
		l, e := d.LowBattery()
		show("lowbattery", l, e)
		leds, e := d.ReceiverLEDs()
		show("receiverLED", leds, e)
		fmt.Printf("  features     %+v\n", d.Features())
		d.Close()
	}
}

func errStr(err error) string {
	if err != nil {
		return "ERR: " + err.Error()
	}
	return ""
}
