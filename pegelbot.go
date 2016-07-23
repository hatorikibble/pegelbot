package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/ChimeraCoder/anaconda"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Configuration struct {
	Logfile                     string
	Up_message                  string
	Down_message                string
	Equal_message               string
	Pegel_API_URL               string
	Twitter_access_token        string
	Twitter_access_token_secret string
	Twitter_consumer_key        string
	Twitter_consumer_secret     string
	Sleep_time_in_hours         int
	Tweet_after_x_hours         int
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
var level_history [3]float64

// init_bot reads the configuration and opens a logger
func init_bot() {

	config_file := os.Getenv("PEGELBOT_CONFIG")
	if config_file == "" {
		fmt.Println("no config file set in environment variable PEGELBOT_CONFIG!")
		os.Exit(1)
	}
	// config
	file, _ := os.Open(config_file)
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

// API Abfragen
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

	// ablegen
	level_history[2] = level_history[1]
	level_history[1] = level_history[0]
	level_history[0] = pegel_float

}

func convert_to_koelsch(conv_number_float float64) int {
	const koelsch_stange_cm = 15
	conv_number_cm := int(conv_number_float * 100)
	conv_number_koelsch := conv_number_cm / koelsch_stange_cm
	logger.Printf("Converted %d cm to %d Kölsch", conv_number_cm, conv_number_koelsch)

	return conv_number_koelsch
}

func get_tendency_message(tendency string) string {
	var file string
	var msg []string
	var num_msg int

	switch tendency {
	case "up":
		file = configuration.Up_message
	case "down":
		file = configuration.Down_message
	case "equal":
		file = configuration.Equal_message
	}

	// init random generator
	rand.Seed(time.Now().UnixNano())

	// read source file
	content, err := ioutil.ReadFile(file)
	check(err)
	//fmt.Print(string(dat))
	msg = strings.Split(string(content), "\n")

	num_msg = len(msg) - 1
	logger.Printf("Found %d elements in %s\n", num_msg, file)

	return msg[rand.Intn(num_msg)]

}

func find_tendency() string {
	var tendency string

	if level_history[1] > level_history[0] {
		tendency = "down"
	} else if level_history[1] < level_history[0] {
		tendency = "up"
	} else if level_history[1] == level_history[0] {
		tendency = "equal"
	}
	logger.Printf("0: %.2f, 1: %.2f, 2: %.2f -> %s", level_history[0], level_history[1], level_history[2], tendency)
	return tendency

}

func write_tendency_tweet(tendency string) {
	logger.Print("Die Tendenz hat sich geändert")
	if tendency == "up" {
		tweet(fmt.Sprintf("Achtung der Rhein beginnt zu steigen! Derzeit liegt der Pegel bei %.2f m #koeln #rhein", level_history[0]))
	} else if tendency == "down" {
		tweet(fmt.Sprintf("Der Rheinpegel sinkt! Derzeit liegen wir bei %.2f m #koeln #rhein", level_history[0]))
	} else if tendency == "equal" {
		tweet(fmt.Sprintf("Alles ruhig. Der Rheinpegel bleibt derzeit bei %.2f m #koeln #rhein", level_history[0]))
	}
}

func write_scheduled_tweet() {
	logger.Print("Zeit für einen Tweet")
	tweet(fmt.Sprintf("Der Rheinpegel ist derzeit %.2f m, das sind %d Kölschstangen #koeln #rhein", level_history[0], convert_to_koelsch(level_history[0])))
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
	var old_tendency string
	var cur_tendency string
	now := time.Now()
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

	fmt.Println(get_tendency_message("up"))

	os.Exit(1)

	for {
		now = time.Now()
		get_water_level()

		old_tendency = cur_tendency
		cur_tendency = find_tendency()
		logger.Printf("Tendenz ist %s (war %s), Hour ist %d, Intervall ist %d", cur_tendency, old_tendency, now.Hour(), configuration.Tweet_after_x_hours)

		// Tendenz ignorieren, solange Historie noch nicht gefuellt ist
		if (cur_tendency != old_tendency) && (level_history[0] > 0 && level_history[1] > 0 && level_history[2] > 0) {
			write_tendency_tweet(cur_tendency)
		} else if now.Hour()%configuration.Tweet_after_x_hours == 0 {
			write_scheduled_tweet()
		}

		sleep_hours := configuration.Sleep_time_in_hours
		logger.Printf("Will go to sleep for %d hours..", sleep_hours)
		time.Sleep(time.Duration(sleep_hours) * time.Hour)

	}

}
