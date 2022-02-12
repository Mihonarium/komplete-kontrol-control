package main

import (
	"encoding/json"
	"fmt"
	"github.com/Mihonarium/go-hid"
	"gitlab.com/gomidi/midi"
	"gitlab.com/gomidi/midi/reader"
	"gitlab.com/gomidi/midi/smf"
	"gitlab.com/gomidi/midi/writer"
	"gitlab.com/gomidi/rtmididrv"
	"io/ioutil"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

type HomeAssistant struct {
	Token string
	URL   string
}

var ha HomeAssistant

func MIDINote(wr writer.ChannelWriter, note uint8, velocity uint8, channel int8) {
	fmt.Println("MIDINote", note, velocity, channel)
	if channel != -1 {
		wr.SetChannel(uint8(channel))
	}
	err := writer.NoteOn(wr, note, velocity)
	must(err)
	time.Sleep(time.Second / 2)
	err = writer.NoteOff(wr, note)
	must(err)
	time.Sleep(time.Second / 2)
}
func (ha *HomeAssistant) CallHomeAssistant(haPath string, haMethod string, haBody string) (string, error) {
	var err error
	var resp *http.Response
	var body []byte
	req, err := http.NewRequest(haMethod, ha.URL+haPath, strings.NewReader(haBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ha.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func getJson(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (ha *HomeAssistant) TTS(message, lang, entity, volume string) error {
	type VolumeChange struct {
		Volume string `json:"volume_level"`
		Entity string `json:"entity_id"`
	}
	_, err := ha.CallHomeAssistant("services/media_player/volume_set", "POST", getJson(VolumeChange{Volume: volume, Entity: entity}))
	if err != nil {
		return err
	}
	type TTS struct {
		Message  string `json:"message"`
		Language string `json:"language"`
		Entity   string `json:"entity_id"`
	}
	_, err = ha.CallHomeAssistant("services/tts/google_translate_say", "POST", getJson(TTS{Message: message, Language: lang, Entity: entity}))
	return err
}

func bytesConc(b1 []byte, b2 []byte) []byte {
	b := make([]byte, len(b1)+len(b2))
	copy(b, b1)
	copy(b[len(b1):], b2)
	return b
}

func (d *Device) WriteKeyColor(key int, color byte) {
	/*key := note + OFFSET*/
	if key < 0 || key >= NB_KEYS {
		fmt.Println("Key out of range", key)
		return
	}
	d.Lock()
	d.CurrentKeysBuffer[key] = color
	d.Unlock()
	d.WriteBuffer()
}
func (d *Device) WriteButtonColor(button int, color byte) {
	if button < 0 || button >= NB_BUTTONS {
		fmt.Println("Button out of range", button)
		return
	}
	d.Lock()
	d.CurrentButtonsBuffer[button] = color
	d.Unlock()
	d.WriteBuffer()
}
func (d *Device) GetDefaultBuffers() ([]byte, []byte) {
	d.Lock()
	defer d.Unlock()
	return d.DefaultButtonsBuffer, d.DefaultKeysBuffer
}

const NB_KEYS = 61
const NB_BUTTONS = 80
const VENDOR_ID = 0x17cc
const PRODUCT_ID = 0x1620
const SERIAL_NUMBER = "0CD3B416"
const OFFSET = -36

var octaveShift = 0

func (d *Device) LightsOff() {
	d.Lock()
	d.CurrentKeysBuffer = make([]byte, 249)
	d.CurrentButtonsBuffer = make([]byte, 249)
	d.Unlock()
	d.WriteBuffer()
}

func (d *Device) SetCurrentKeysAsDefault() {
	d.Lock()
	defer d.Unlock()
	copy(d.DefaultKeysBuffer, d.CurrentKeysBuffer)
}

func (d *Device) SetCurrentButtonsAsDefault() {
	d.Lock()
	defer d.Unlock()
	copy(d.DefaultButtonsBuffer, d.CurrentButtonsBuffer)
}

// Device info
// 0x80: set colors of control buttons
// 0: M, 1: S, 2-9: top row, 10 - wheel left, 11 - wheel top, 12 - wheel bottom, 13 - wheel right
// 14-41 - buttons (on/off only), 42-43 - don't respond
// 44-68 - strip under the two left wheels

// 0x81: set colors of keys
// 0-61: keys

// Colors
// 0-3 - black
// 4-67 - colors
// 68-71 - white
// 71 - 127 - repeating white

const (
	M_BUTTON      = 0
	S_BUTTON      = 1
	TOP_ROW_START = 2
	WHEEL_LEFT    = 10
	WHEEL_TOP     = 11
	WHEEL_BOTTOM  = 12
	WHEEL_RIGHT   = 13
	STRIP_START   = 44
)

func (d *Device) WriteBuffer() {
	go func() {
		d.Lock()
		d.WriteToDevice(0x81, d.CurrentKeysBuffer)
		d.WriteToDevice(0x80, d.CurrentButtonsBuffer)
		d.Unlock()
	}()
}

type Color struct {
	// 0 - black, 1 - red (brightness 2 is the brightest red), 2 - orangish, 3 - orange, 4-5 - yellow, 6 - light green,
	// 7 - green, 8 - sea color , 9 - light blue (brightness 2 is the brightest blue), 10 - blue, 11 - dark blue,
	// 12 - purple, 13 - purplish, 13 - pink, 14 - white
	Color      uint8
	Brightness uint8
}

const (
	BLACK      uint8 = 0
	RED        uint8 = 1
	ORANGE0    uint8 = 2
	ORANGE     uint8 = 3
	YELLOW     uint8 = 4
	YELLOW2    uint8 = 5
	LIGHTGREEN uint8 = 6
	GREEN      uint8 = 7
	SEA        uint8 = 8
	LIGHTBLUE  uint8 = 9
	BLUE       uint8 = 10
	DARKBLUE   uint8 = 11
	PURPLE     uint8 = 12
	PURPLE2    uint8 = 13
	PINK       uint8 = 14
	PINK2      uint8 = 15
	PINK3      uint8 = 16
	WHITE      uint8 = 17
)

func GetColor(c Color) byte {
	return c.Color*4 + c.Brightness
}

func (d *Device) WriteAllKeys(c Color) {
	buf := make([]Color, NB_KEYS)
	for i := 0; i < NB_KEYS; i++ {
		buf[i] = c
	}
	d.WriteKeys(buf)
}

func (d *Device) WriteAll(color Color) {
	d.WriteAllKeys(color)
	d.FillColorfulButtons(color)
}

func (d *Device) FillColorfulButtons(color Color) {
	buf := make([]Color, 69)
	for i := 0; i < 69; i++ {
		if i >= 14 && i <= 43 {
			buf[i] = Color{0, 0}
		} else {
			buf[i] = color
		}
	}
	d.WriteButtons(buf)
}

func (d *Device) WriteKeys(colors []Color) {
	go func() {
		d.Lock()
		for i, c := range colors {
			d.CurrentKeysBuffer[i] = GetColor(c)
		}
		d.WriteToDevice(0x81, d.CurrentKeysBuffer)
		d.Unlock()
	}()
}

func (d *Device) WriteButtons(colors []Color) {
	go func() {
		d.Lock()
		for i, c := range colors {
			d.CurrentButtonsBuffer[i] = GetColor(c)
		}
		d.WriteToDevice(0x80, d.CurrentButtonsBuffer)
		d.Unlock()
	}()
}

func (d *Device) WriteToDevice(where byte, data []byte) {
	d.Device.Write(bytesConc([]byte{where}, data))
}

var ShowingScenes = false

func (d *Device) ShowScenes() {
	if ShowingScenes {
		d.ShowDefault()
		return
	}
	ShowingScenes = true
	d.Lock()
	d.CurrentButtonsBuffer[TOP_ROW_START+1] = GetColor(Color{RED, 2})
	d.CurrentButtonsBuffer[TOP_ROW_START+2] = GetColor(Color{WHITE, 3})
	d.CurrentButtonsBuffer[TOP_ROW_START+3] = GetColor(Color{BLACK, 2})
	d.CurrentButtonsBuffer[TOP_ROW_START+4] = GetColor(Color{GREEN, 2})
	d.CurrentButtonsBuffer[TOP_ROW_START+5] = GetColor(Color{BLUE, 2})
	d.Unlock()
	d.WriteBuffer()
}
func (d *Device) ShowDefault() {
	ShowingScenes = false
	d.Lock()
	for i := 0; i < 8; i++ {
		d.CurrentButtonsBuffer[TOP_ROW_START+i] = d.DefaultButtonsBuffer[TOP_ROW_START+i]
	}
	d.Unlock()
	d.WriteBuffer()
}

func (d *Device) SendScene(scene int) {
	call := "services/script/turn_on"
	body := ""
	switch scene {
	case 0:
		body = getJson(map[string]string{"entity_id": "script.lights_red"})
		d.WriteAll(Color{RED, 1})
		d.SetCurrentKeysAsDefault()
		d.SetCurrentButtonsAsDefault()
	case 1:
		body = getJson(map[string]string{"entity_id": "script.lights_white"})
		d.WriteAll(Color{WHITE, 2})
		d.SetCurrentKeysAsDefault()
		d.SetCurrentButtonsAsDefault()
	case 2:
		body = getJson(map[string]string{"entity_id": "script.lights_off"})
		d.WriteAll(Color{BLACK, 2})
		d.SetCurrentKeysAsDefault()
		d.SetCurrentButtonsAsDefault()
	case 3:
		call = "services/light/turn_on"
		body = getJson(map[string]string{"entity_id": "light.bedroom_lights", "brightness_pct": "100", "color_name": "green"})
		d.WriteAll(Color{GREEN, 2})
		d.SetCurrentKeysAsDefault()
		d.SetCurrentButtonsAsDefault()
	case 4:
		call = "services/light/turn_on"
		body = getJson(map[string]string{"entity_id": "light.bedroom_lights", "brightness_pct": "100", "color_name": "blue"})
		d.WriteAll(Color{BLUE, 2})
		d.SetCurrentKeysAsDefault()
		d.SetCurrentButtonsAsDefault()
	}
	go func() {
		_, err := ha.CallHomeAssistant(call, "POST", body)
		if err != nil {
			fmt.Println("Error calling home assistant", err)
		}
	}()
	fmt.Println("Sent to HA", call, body)
}
func ChangeBrightness(brightnessPct int) {
	call := "services/light/turn_on"
	body := getJson(map[string]string{"entity_id": "light.bedroom_lights", "brightness_pct": strconv.Itoa(brightnessPct)})
	go func() {
		_, err := ha.CallHomeAssistant(call, "POST", body)
		if err != nil {
			fmt.Println("Error calling home assistant", err)
		}
	}()
	fmt.Println("Sent to HA", call, body)
}
func (d *Device) LaunchRickRoll() {
	call := "services/automation/trigger"
	body := getJson(map[string]string{"entity_id": "automation.rickroll"})
	go func() {
		_, err := ha.CallHomeAssistant(call, "POST", body)
		if err != nil {
			fmt.Println("Error calling home assistant", err)
		}
		go d.ColorfulLights(1)
	}()
	fmt.Println("Sent to HA", call, body)
}

func (d *Device) ColorfulLights(mode int) {
	//ToDO: rainbow and other animation? reacting to music?
	d.Lock()
	p := d.PlayingAnimation
	d.Unlock()
	if p {
		return
	}
	d.Lock()
	d.PlayingAnimation = true
	d.Unlock()

	if mode == 0 {
		go func() {
			t := time.NewTicker(time.Second / 30)
			previousColor := Color{1, 2}
			currentColor := Color{1, 2}
			var i int
			for range t.C {
				d.Lock()
				d.CurrentKeysBuffer[30+i] = GetColor(currentColor)
				d.CurrentKeysBuffer[30-i] = GetColor(currentColor)
				d.CurrentButtonsBuffer[68-i] = GetColor(previousColor)
				if i < 8 {
					d.CurrentButtonsBuffer[5-i/2] = GetColor(currentColor)
					d.CurrentButtonsBuffer[5+i/2+1] = GetColor(currentColor)
				}
				if i == 13 {
					d.CurrentButtonsBuffer[1] = GetColor(currentColor)
				}
				if i == 14 {
					d.CurrentButtonsBuffer[0] = GetColor(currentColor)
				}
				d.Unlock()
				d.WriteBuffer()
				i++
				if i > NB_KEYS/2 {
					previousColor = currentColor
					i = 0
					currentColor.Color++
					if currentColor.Color == 16 {
						currentColor.Color = 1
						//currentColor.Brightness++
					}
				}
			}
		}()
	}

	if mode == 1 {
		// time.Sleep(time.Second * 1)
		d.LightsOff()
		d.SetCurrentKeysAsDefault()
		go func() {
			t := time.NewTicker(time.Minute / 131)
			go func() {
				time.Sleep(time.Minute*3 + time.Second*32)
				t.Stop()
			}()
			currentPlayTOn := false
			for range t.C {
				if currentPlayTOn {
					currentPlayTOn = false
					d.Lock()
					d.CurrentButtonsBuffer[29] = GetColor(Color{WHITE, 2})
					d.Unlock()
					d.WriteBuffer()
				} else {
					currentPlayTOn = true
					d.Lock()
					d.CurrentButtonsBuffer[29] = GetColor(Color{BLACK, 2})
					d.Unlock()
					d.WriteBuffer()
				}
			}
		}()
		resolution := smf.MetricTicks(404)
		rd := reader.New(
			reader.NoLogger(),
			// write every message to the out port
			reader.NoteOn(func(p *reader.Position, channel, key, vel uint8) {
				fmt.Printf("Track: %v Pos: %v NoteOn (ch %v: key %v)\n", p.Track, p.AbsoluteTicks, channel, key)
				time.Sleep(resolution.Duration(113, p.DeltaTicks))
				d.NoteOnCallback(key, channel, vel)
			}),
			reader.NoteOff(func(p *reader.Position, channel, key, vel uint8) {
				fmt.Printf("Track: %v Pos: %v NoteOff (ch %v: key %v)\n", p.Track, p.AbsoluteTicks, channel, key)
				time.Sleep(resolution.Duration(113, p.DeltaTicks))
				d.NoteOffCallback(key, channel)
			}),
		)
		err := reader.ReadSMFFile(rd, "Never-Gonna-Give-You-Up-3.mid")
		must(err)
	}
}

func (d *Device) ChangesCallback(field string, i int, oldValue, newValue interface{}) {
	if field == "BottomRowTouched" || field == "TopRowButtons" || field == "SPressed" {
		switch i {
		case 0:
			if newValue.(bool) {
				d.ShowScenes()
			} else {
				d.ShowScenes()
				brightness := d.State.BottomRowPitch[0] / 10
				if brightness > 95 {
					brightness = 100
				}
				if brightness < 5 {
					brightness = 0
				}
				ChangeBrightness(brightness)
			}
		default:
			if newValue.(bool) {
				d.SendScene(i - 1)
			}
		}
	} else if field == "OctaveDecreasePressed" {
		if newValue.(bool) && octaveShift < 3 {
			octaveShift++
		}
	} else if field == "OctaveIncreasePressed" {
		if newValue.(bool) && octaveShift > -3 {
			octaveShift--
		}
	} else if field == "PlayPressed" {
		if newValue.(bool) {
			d.LaunchRickRoll()
		}
	} else if field == "RecPressed" {
		d.ColorfulLights(0)
	} else {
		//fmt.Println(field)
	}
}

func (d *Device) ParseDeviceState(state []byte) DeviceState {
	newState := *d.State
	if state[0] == 1 {
		topRowPressed := int(state[1])
		newState.TopRowButtons = []bool{
			topRowPressed&16 > 0,
			topRowPressed&32 > 0,
			topRowPressed&64 > 0,
			topRowPressed&128 > 0,
			topRowPressed&1 > 0,
			topRowPressed&2 > 0,
			topRowPressed&4 > 0,
			topRowPressed&8 > 0,
		}
		bottomRowTouchedBits := int(state[7])
		newState.BottomRowTouched = []bool{
			bottomRowTouchedBits&128 > 0,
			bottomRowTouchedBits&64 > 0,
			bottomRowTouchedBits&32 > 0,
			bottomRowTouchedBits&16 > 0,
			bottomRowTouchedBits&8 > 0,
			bottomRowTouchedBits&4 > 0,
			bottomRowTouchedBits&2 > 0,
			bottomRowTouchedBits&1 > 0,
		}
		bottomRowPitch := make([]int, 8)
		for i := 0; i < 8; i++ {
			bottomRowPitch[i] = int(state[i*2+10]) + 256*int(state[i*2+11])
		}
		newState.BottomRowPitch = bottomRowPitch
		newState.SelectorTouched = int(state[6])&4 > 0
		newState.SelectorPressed = int(state[6])&8 > 0
		newState.SelectorLeft = int(state[6])&16 > 0
		newState.SelectorTop = int(state[6])&32 > 0
		newState.SelectorBottom = int(state[6])&64 > 0
		newState.SelectorRight = int(state[6])&128 > 0
		newState.SelectorPitch = state[30]

		/*for i := 0; i < 8; i++ {
			if newState.BottomRowTouched[i] {
				buttonLightsBuffer[TOP_ROW_START+i] = GetColor(Color{uint8(bottomRowPitch[i]/5) % 16, uint8(bottomRowPitch[i]/80) % 4})
			} else {
				buttonLightsBuffer[TOP_ROW_START+i] = GetColor(Color{RED, 2})
			}
		}*/
		d.Lock()
		for i := 0; i < 4; i++ {
			if newState.SelectorTouched {
				d.CurrentButtonsBuffer[WHEEL_LEFT+i] = GetColor(Color{uint8(newState.SelectorPitch), 2})
			} else {
				d.CurrentButtonsBuffer[WHEEL_LEFT+i] = d.DefaultButtonsBuffer[WHEEL_LEFT+i]
			}
		}
		d.WriteToDevice(0x80, d.CurrentButtonsBuffer)
		d.Unlock()
		// WriteBuffer(d)

		newState.MPressed = int(state[4])&1 > 0
		newState.SPressed = int(state[4])&2 > 0

		newState.ShiftPressed = int(state[2])&128 > 0 // Additional Behaviour
		newState.ScalePressed = int(state[2])&8 > 0
		newState.ARPPressed = int(state[2])&4 > 0
		newState.UndoPressed = int(state[2])&64 > 0
		newState.QuantizePressed = int(state[2])&2 > 0
		newState.AutoPressed = int(state[2])&1 > 0

		newState.ScenePressed = int(state[4])&4 > 0
		newState.PatternPressed = int(state[4])&8 > 0
		newState.TrackPressed = int(state[4])&16 > 0
		newState.KeyModePressed = int(state[4])&64 > 0
		newState.ClearPressed = int(state[4])&32 > 0

		newState.PresetUpPressed = int(state[3])&16 > 0
		newState.PresetDownPressed = int(state[3])&64 > 0
		newState.LeftPressed = int(state[3])&128 > 0
		newState.RightPressed = int(state[3])&32 > 0

		newState.LoopPressed = int(state[2])&32 > 0 // Additional Behaviour
		newState.MetroPressed = int(state[3])&8 > 0
		newState.TempoPressed = int(state[3])&4 > 0
		newState.PlayPressed = int(state[2])&16 > 0 // Additional Behaviour
		newState.RecPressed = int(state[3])&2 > 0
		newState.StopPressed = int(state[3])&1 > 0 // Additional Behaviour

		newState.BrowserPressed = int(state[5])&4 > 0
		newState.PlugInPressed = int(state[5])&2 > 0
		newState.MixerPressed = int(state[5])&1 > 0
		newState.InstancePressed = int(state[5])&16 > 0 // Additional Behaviour
		newState.MIDIPressed = int(state[5])&32 > 0     // Additional Behaviour
		newState.SetupPressed = int(state[5])&8 > 0     // Additional Behaviour

		newState.FixedVelPressed = int(state[8])&4 > 0 // Additional Behaviour
		newState.OctaveDecreasePressed = int(state[8])&1 > 0
		newState.OctaveIncreasePressed = int(state[8])&2 > 0

		newState.LeftWheelPitch = state[35]
		newState.RightWheelPitch = (int(state[34])-32)*256 + int(state[33])
		newState.StripValue = state[37]
	} else if state[0] == 170 {
		newState.LeftWheelPitch = state[35]
		newState.RightWheelPitch = (int(state[34])-32)*256 + int(state[33])
		newState.StripValue = state[37]
	} else {
		fmt.Println("Unknown device state", state)
	}
	d.ReflectChanges(reflect.ValueOf(*d.State), reflect.ValueOf(newState), d.ChangesCallback)
	return newState
}

func (d *Device) ReflectChanges(vOld, vNew reflect.Value, callback func(string, int, interface{}, interface{})) {
	for i := 0; i < vNew.NumField(); i++ {
		if vNew.Field(i).Kind() == reflect.Slice || vNew.Field(i).Kind() == reflect.Array {
			if vOld.Field(i).IsNil() {
				anyNew := false
				for j := 0; j < vNew.Field(i).Len(); j++ {
					if !vNew.Field(i).Index(j).IsZero() {
						if !anyNew {
							anyNew = true
							fmt.Printf("%s: new: ", vNew.Type().Field(i).Name)
						} else {
							fmt.Printf(", ")
						}
						fmt.Printf("[%d]: %v", j, vNew.Field(i).Index(j))
						callback(vNew.Type().Field(i).Name, j, nil, vNew.Field(i).Index(j).Interface())
					}
				}
				if anyNew {
					fmt.Println()
				}
			} else {
				anyChanges := false
				for j := 0; j < vNew.Field(i).Len(); j++ {
					if vNew.Field(i).Index(j).Interface() != vOld.Field(i).Index(j).Interface() { // assumes constant length of arrays once initialized
						if !anyChanges {
							anyChanges = true
							fmt.Printf("%s: ", vNew.Type().Field(i).Name)
						} else {
							fmt.Printf(", ")
						}
						callback(vNew.Type().Field(i).Name, j, vOld.Field(i).Index(j).Interface(), vNew.Field(i).Index(j).Interface())
						fmt.Printf("[%d]: %v -> %v", j, vOld.Field(i).Index(j), vNew.Field(i).Index(j))
					}
				}
				if anyChanges {
					fmt.Println()
				}
			}
		} else {
			if vNew.Field(i).Interface() != vOld.Field(i).Interface() {
				fmt.Printf("%s: %v -> %v\n", vNew.Type().Field(i).Name, vOld.Field(i), vNew.Field(i))
				callback(vNew.Type().Field(i).Name, 0, vOld.Field(i).Interface(), vNew.Field(i).Interface())
			}
		}
		vNew.Field(i).Interface()
	}
}

type DeviceState struct {
	TopRowButtons                                            []bool
	BottomRowTouched                                         []bool
	BottomRowPitch                                           []int
	SelectorTouched                                          bool
	SelectorPressed                                          bool
	SelectorPitch                                            uint8
	SelectorLeft, SelectorTop, SelectorBottom, SelectorRight bool

	MPressed, SPressed bool

	ShiftPressed, ScalePressed, ARPPressed, UndoPressed, QuantizePressed, AutoPressed bool

	ScenePressed, PatternPressed, TrackPressed, KeyModePressed, ClearPressed bool

	PresetUpPressed, PresetDownPressed, LeftPressed, RightPressed bool

	LoopPressed, MetroPressed, TempoPressed, PlayPressed, RecPressed, StopPressed bool

	BrowserPressed, PlugInPressed, MixerPressed, InstancePressed, MIDIPressed, SetupPressed bool

	FixedVelPressed, OctaveDecreasePressed, OctaveIncreasePressed bool

	LeftWheelPitch, StripValue uint8
	RightWheelPitch            int
}

var haToken string = ""

func (d *Device) NoteOnCallback(note, channel, velocity uint8) {
	brightness := uint8(1)
	if velocity > 40 {
		brightness = 2
	}
	color := uint8(1)
	switch channel {
	case 0:
		color = BLUE
	case 6:
		color = GREEN
	case 11:
		color = DARKBLUE
	case 12:
		color = GREEN
	case 14:
		color = PINK
	default:
		color = channel + 1
		fmt.Println("!!!", channel)
	}
	d.WriteKeyColor(int(note)+OFFSET+octaveShift*12, GetColor(Color{color, brightness}))
	fmt.Printf("NoteOn: %d, %d, %d\n", note, channel, velocity)

}
func (d *Device) NoteOffCallback(note, channel uint8) {
	//ToDo: stacking all the on and off notes so if there are two at the same time but on different channels, we show the playing notes
	fmt.Printf("NoteOff: %d, %d\n", note, channel)
	key := int(note) + OFFSET + octaveShift*12
	if key < 0 {
		return
	}
	d.Lock()
	defaultColor := d.DefaultKeysBuffer[key]
	d.Unlock()
	d.WriteKeyColor(key, defaultColor)
}

type Device struct {
	Device               *hid.Device
	State                *DeviceState
	DefaultColor         Color
	DefaultKeysBuffer    []byte
	DefaultButtonsBuffer []byte
	CurrentKeysBuffer    []byte
	CurrentButtonsBuffer []byte
	PlayingAnimation     bool
	*sync.Mutex
}

func main() {
	ha = HomeAssistant{
		Token: haToken,
		URL:   "http://192.168.1.2:8123/api/",
	}
	err := hid.Init()
	must(err)
	dHid, err := hid.Open(VENDOR_ID, PRODUCT_ID, SERIAL_NUMBER)
	must(err)
	dHid.Write([]byte{0xa0})
	d := Device{
		Device:               dHid,
		State:                &DeviceState{},
		DefaultColor:         Color{},
		DefaultKeysBuffer:    make([]byte, 249),
		DefaultButtonsBuffer: make([]byte, 249),
		CurrentKeysBuffer:    make([]byte, 249),
		CurrentButtonsBuffer: make([]byte, 249),
		Mutex:                &sync.Mutex{},
	}
	d.LightsOff()
	defer d.Device.Close()
	d.WriteAll(Color{RED, 1})

	drv, err := rtmididrv.New()
	must(err)
	defer drv.Close()

	/*outs, err := drv.Outs()
	must(err)
	ins, err := drv.Ins()

	must(err)
	var out midi.Out*/
	// ToDo: find the LoopBe input by the name substring
	ins, err := drv.Ins()
	must(err)
	var in midi.In
	for _, o := range ins {
		fmt.Println(o.String(), o.Number())
		if strings.Contains(o.String(), "LoopBe Internal MIDI") {
			//in = o
			break
		}
	}
	if in == nil {
		for _, o := range ins {
			fmt.Println(o.String(), o.Number())
			if strings.Contains(o.String(), "KOMPLETE KONTROL") {
				in = o
			}
		}
	}
	if in == nil {
		fmt.Println("No input found")
		return
	}

	//in, err = midi.OpenIn(drv, -1, "LoopBe Internal MIDI 3")
	//must(err)

	/*for _, o := range outs {
		if o.String() == "LoopBe Internal MIDI 4" {
			out = o
		} else {
			fmt.Println(o.String(), o.Number())
		}
	}
	must(out.Open())
	defer out.Close()

	fmt.Println(out.String())
	wr := writer.New(out)
	// MIDINote channels:
	// green: 0, 7-10, 12-15, light green: 6
	// blue: 2-5, 11, violetish: 1
	MIDINote(wr, 60, 20, 1)
	MIDINote(wr, 60, 20, 6) // light green
	//in, err := midi.OpenIn(drv, 0, "Test Golang MIDI Output")
	//drv.OpenVirtualIn()
	// must(in.Open())*/
	defer in.Close()
	/*rd := reader.New(
		reader.NoLogger(),
		// write every message to the out port
		reader.Each(func(pos *reader.Position, msg midi.Message) {
			switch v := msg.(type) {
			case channel.NoteOn:
				d.NoteOnCallback(v.Key(), v.Channel(), v.Velocity())
			case channel.NoteOff:
				d.NoteOffCallback(v.Key(), v.Channel())
			}
			fmt.Printf("got %s\n", msg)
		}),
	)
	err = rd.ListenTo(in)
	must(err)*/
	for {
		buffer := make([]byte, 42)
		n, err := d.Device.Read(buffer)
		must(err)
		if n == 0 {
			continue
		}
		state := d.ParseDeviceState(buffer)
		d.State = &state
	}
}

func must(err error) {
	if err != nil {
		panic(err.Error())
	}
}
