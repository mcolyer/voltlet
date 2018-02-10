## Voltlet

It lets you control [Etekcity Voltson] smart plugs via your local server rather than Etekcity's cloud.

### Getting started

* You need to redirect server2.vesync.com to the computer running
  voltlet.
* Build it:
  * For your platform: `go build -o voltlet-armhf main.go`
  * To cross compile for a raspberry pi 3: `GOOS=linux GOARCH=arm GOARM=6 go build -o voltlet-armhf main.go`
* Run it: `./voltlet -mqtt-broker $MQTT_BROKER -mqtt-user $MQTT_USER -mqtt-password $MQTT_PASSWORD`
* Restart your plugs so that they connect to the local server.

#### MQTT

* Send "true" to turn on the plug to `/voltson/{plug-uuid}`
* Send "false" to turn off the plug to `/voltson/{plug-uuid}`
* Plugs send "true" or "false" to `/voltson/{plug-uuid}/state` once they've actually changed state. Messages are retained.
* Plugs send "online" or "offline" to `/voltson/{plug-uuid}/availability` depending on whether they are connected. Messages are retained.


### TODO

* [ ] Improve the readme
* [ ] Make a build script
* [ ] Report energy usage via MQTT
* [ ] Make it more robust against disconnects from plugs

### References

* [vesync-wsproxy](https://github.com/itsnotlupus/vesync-wsproxy) this project just proxies the connect to the cloud server to spy on what happens. I'd rather keep my data local.
* This [project](https://github.com/travissinnott/outlet) attempted to do something similar but wasn't fully implemented. That said it has great notes about the line protocol


[Etekcity Voltson]: https://www.amazon.com/gp/product/B06XSTJST6/ref=as_li_tl?ie=UTF8&camp=1789&creative=9325&creativeASIN=B06XSTJST6&linkCode=as2&tag=matcol01-20&linkId=ab8750e61f7f9723ddaa60cb56d0df82
