// probe prints the state of connected devices without changing anything.
package main

import (
	"fmt"
	"log"

	"opensynapse/internal/razer"
)

func main() {
	devs, err := razer.Scan()
	if err != nil {
		log.Fatal(err)
	}
	if len(devs) == 0 {
		fmt.Println("no supported device found")
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
		d.Close()
	}
}

func errStr(err error) string {
	if err != nil {
		return "ERR: " + err.Error()
	}
	return ""
}
