package gol

// Params provides the details of how to run the Game of Life and which image to load.
type Params struct {
	Turns       int
	Threads     int
	ImageWidth  int
	ImageHeight int
	Continue    bool //identify whether the new client will continue from last world or reset
}

// Run starts the processing of Game of Life. It should initialise channels and goroutines.
func Run(p Params, events chan<- Event, keyPresses <-chan rune) {

	//	TODO: Put the missing channels in here.
	ioCom := make(chan ioCommand)
	ioIdle := make(chan bool)
	ioFilename := make(chan string)
	ioIn := make(chan uint8)
	ioOut := make(chan uint8)

	ioChannels := ioChannels{
		command:  ioCom,
		idle:     ioIdle,
		filename: ioFilename,
		output:   ioOut,
		input:    ioIn,
	}
	go startIo(p, ioChannels)

	distributorChannels := distributorChannels{
		events:       events,
		ioCommand:    ioCom,
		ioIdle:       ioIdle,
		ioFilename:   ioFilename,
		ioOutput:     ioOut,
		ioInput:      ioIn,
		ioKeyPresses: keyPresses,
	}
	distributor(p, distributorChannels)

}
