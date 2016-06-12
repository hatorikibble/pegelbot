package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/ChimeraCoder/anaconda"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"syscall"
)

type Configuration struct {
	Logfile                     string
	Pegel_API_URL               string
	Twitter_access_token        string
	Twitter_access_token_secret string
	Twitter_consumer_key        string
	Twitter_consumer_secret     string
	Debug                       int
}

type Hochwasserpegel struct {
	Datum   string
	Uhrzeit string
	Pegel   string
	Grafik  string
}

var logfile *os.File
var err error
var logger *log.Logger
var configuration Configuration

// init_bot opens a log file,
func init_bot() {

	// config
	file, _ := os.Open("config.json")
	decoder := json.NewDecoder(file)
	configuration = Configuration{}
	err := decoder.Decode(&configuration)
	check(err)

	// logging
	logfile, err = os.OpenFile(configuration.Logfile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	check(err)
	logger = log.New(logfile, "", log.Ldate|log.Ltime|log.Lshortfile)
	logger.Print("Started...")
}

func get_water_level() {

	logger.Print("Querying ", configuration.Pegel_API_URL)
	response, err := http.Get(configuration.Pegel_API_URL)
	check(err)
	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	check(err)
	hwpegel := new(Hochwasserpegel)
	err = xml.Unmarshal(body, hwpegel)
	check(err)
	logger.Printf("Pegel ist %s am %s um %s", hwpegel.Pegel, hwpegel.Datum, hwpegel.Uhrzeit)
	// Number format
	rp := regexp.MustCompile(",")
	pegel_number := rp.ReplaceAllString(hwpegel.Pegel, ".")
	pegel_float, err := strconv.ParseFloat(pegel_number, 64)
	check(err)
	tweet(fmt.Sprintf("Der Rheinpegel ist derzeit %s m, das sind %d Kölschstangen", hwpegel.Pegel, convert_to_koelsch(pegel_float)))
}

func convert_to_koelsch(conv_number_float float64) int {
	const koelsch_stange_cm = 15
	conv_number_cm := int(conv_number_float * 100)
	conv_number_koelsch := conv_number_cm / koelsch_stange_cm
	log.Printf("Converted %d cm to %d Kölsch", conv_number_cm, conv_number_koelsch)

	return conv_number_koelsch
}

// check panics if an error is detected
func check(e error) {
	if e != nil {
		panic(e)
	}
}

func tweet(tweet_text string) {
	// twitter api
	anaconda.SetConsumerKey(configuration.Twitter_consumer_key)
	anaconda.SetConsumerSecret(configuration.Twitter_consumer_secret)
	// I don't know about any possible timeout, therefore
	// initialize new for every tweet
	api := anaconda.NewTwitterApi(configuration.Twitter_access_token, configuration.Twitter_access_token_secret)

	if configuration.Debug == 1 {
		logger.Printf("DEBUG-MODE! I am not posting '%s'!", tweet_text)
	} else {
		tweet, err := api.PostTweet(tweet_text, nil)
		if err != nil {
			logger.Printf("Problem posting '%s': %s", tweet_text, err)
		} else {
			logger.Printf("Tweet %s posted for user %s", tweet_text, tweet.User.ScreenName)
		}
	}
}

func main() {

	// catch interrupts
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	signal.Notify(c, syscall.SIGTERM)
	go func() {
		<-c
		logger.Print("ended...")
		os.Exit(1)
	}()

	init_bot()
	get_water_level()

}
