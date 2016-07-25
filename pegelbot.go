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
	Min_change_cm               int
	High_water_cm               int
	High_water_level_1_cm       int
	High_water_level_2_cm       int
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
var level_history [3]int64
var variance int64
var last_tweet_timestamp int64

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
	// Pegel in Meter, aber auf cm genau
	// Komma raus, centimeter da..
	rp := regexp.MustCompile(",")
	pegel_number := rp.ReplaceAllString(hwpegel.Pegel, "")
	pegel_int, err := strconv.ParseInt(pegel_number, 10, 64)
	check(err)

	// ablegen
	level_history[2] = level_history[1]
	level_history[1] = level_history[0]
	level_history[0] = pegel_int

	// Streuung berechnen, wenn genuegend Werte da sind
	if level_history[2] > 0 {
		variance = level_history[2] - level_history[0]
	} else {
		variance = 0
	}
	// Absoluten Wert verwenden
	if variance < 0 {
		variance = -variance
	}

}

func convert_to_koelsch(conv_number_cm int64) int64 {
	const koelsch_stange_cm = 15
	conv_number_koelsch := conv_number_cm / koelsch_stange_cm
	logger.Printf("Converted %d cm to %d Kölsch", conv_number_cm, conv_number_koelsch)

	return conv_number_koelsch
}

func cm_to_m(cm int64) float64 {
	m := float64(cm) / 100
	return m
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
	logger.Printf("0: %d, 1: %d, 2: %d -> %s (%d)", level_history[0], level_history[1], level_history[2], tendency, variance)
	return tendency

}

func write_tendency_tweet(tendency string) {
	var slogan string
	var tweet_msg string

	logger.Print("Kölscher Tendenztweet")
	slogan = get_tendency_message(tendency)
	if tendency == "up" {
		tweet_msg = fmt.Sprintf("%s Der Rheinpegel steigt! Derzeit liegen wir bei %.2f m #koeln #rhein", slogan, cm_to_m(level_history[0]))
	} else if tendency == "down" {
		tweet_msg = fmt.Sprintf("%s Der Rheinpegel sinkt! Derzeit liegen wir bei %.2f m #koeln #rhein", slogan, cm_to_m(level_history[0]))
	} else if tendency == "equal" {
		tweet_msg = fmt.Sprintf("%s Der Rheinpegel bleibt derzeit bei %.2f m #koeln #rhein", slogan, cm_to_m(level_history[0]))
	}
	logger.Printf("Message has %d characters", len(tweet_msg))
	tweet(tweet_msg)
}

func write_scheduled_tweet() {
	logger.Print("Zeit für einen Tweet")
	tweet(fmt.Sprintf("Der Rheinpegel ist derzeit %.2f m, das sind %d Kölschstangen #koeln #rhein", cm_to_m(level_history[0]), convert_to_koelsch(level_history[0])))
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
		last_tweet_timestamp = int64(time.Now().Unix())
	} else {
		tweet, err := api.PostTweet(tweet_text, nil)
		if err != nil {
			logger.Printf("Problem posting '%s': %s", tweet_text, err)
		} else {
			logger.Printf("Tweet %s posted for user %s", tweet_text, tweet.User.ScreenName)
			last_tweet_timestamp = int64(time.Now().Unix())
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

	for {
		now = time.Now()
		tweet_date_lower_unix := now.Add(-(time.Duration(configuration.Tweet_after_x_hours) * time.Second)).Unix()
		get_water_level()

		old_tendency = cur_tendency
		cur_tendency = find_tendency()
		logger.Printf("Tendenz ist %s (war %s), Varianz ist %d, Hour ist %d, Intervall ist %d Stunden", cur_tendency, old_tendency, variance, now.Hour(), configuration.Tweet_after_x_hours)

		// Streungslimit ueberschritten?
		if variance > int64(configuration.Min_change_cm) {
			logger.Printf("Varianz %d ist größer als Limit %d", variance, configuration.Min_change_cm)
			write_tendency_tweet(cur_tendency)
		} else if int64(tweet_date_lower_unix) > last_tweet_timestamp {
			// wiedermal Zeit fuer einen Tweet
			write_scheduled_tweet()
		}
		sleep_hours := configuration.Sleep_time_in_hours
		logger.Printf("Will go to sleep for %d hours..", sleep_hours)
		time.Sleep(time.Duration(sleep_hours) * time.Hour)

	}

}
