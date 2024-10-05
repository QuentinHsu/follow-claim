/*
 * @Author: Vincent Yang
 * @Date: 2024-09-30 02:01:59
 * @LastEditors: Vincent Young
 * @LastEditTime: 2024-09-30 10:25:36
 * @FilePath: /follow-claim/claim.go
 * @Telegram: https://t.me/missuo
 * @GitHub: https://github.com/missuo
 *
 * Copyright © 2024 by Vincent, All Rights Reserved.
 */

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/robfig/cron/v3"
)

// Config holds all configuration variables
type Config struct {
	Cookie         string
	UserID         string
	BarkURL        string
	TelegramToken  string
	TelegramChatID string
	ScheduledTime  string
	TimeZoneOffset int
}

// Client wraps the HTTP client and other dependencies
type Client struct {
	HTTPClient *http.Client
	Config     *Config
}

// NewClient initializes and returns a Client
func NewClient(config *Config) *Client {
	return &Client{
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		Config: config,
	}
}

// sendToTelegram sends a message to Telegram
func (c *Client) sendToTelegram(message string) error {
	title := "*Follow Claim*"
	fullMessage := fmt.Sprintf("%s\n%s", title, message)

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", c.Config.TelegramToken)
	payload := map[string]string{
		"chat_id":    c.Config.TelegramChatID,
		"text":       fullMessage,
		"parse_mode": "Markdown",
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Telegram payload: %w", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), "POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create Telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Telegram message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Telegram API returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// sendToBark sends a notification to Bark if enabled
func (c *Client) sendToBark(message string) {
	if c.Config.BarkURL == "" {
		return
	}

	url := c.Config.BarkURL
	payload := map[string]string{
		"body": message,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal Bark payload: %v", err)
		return
	}

	req, err := http.NewRequestWithContext(context.Background(), "POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		log.Printf("Failed to create Bark request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		log.Printf("Failed to send Bark notification: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("Bark API returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}
}

// parseScheduledTime parses the scheduled time in "HH:MM" format
func parseScheduledTime(scheduledTime string) (hour, minute int, err error) {
	parts := bytes.Split([]byte(scheduledTime), []byte(":"))
	if len(parts) != 2 {
		return 0, 0, errors.New("time must be in HH:MM format")
	}
	hour, err = strconv.Atoi(string(parts[0]))
	if err != nil {
		return
	}
	minute, err = strconv.Atoi(string(parts[1]))
	return
}

// signFollow handles the claim daily action
func (c *Client) signFollow() (string, error) {
	url := "https://api.follow.is/wallets/transactions/claim_daily"
	payload := map[string]string{"csrfToken": extractCSRFToken(c.Config.Cookie)}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal claim payload: %w", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), "POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", fmt.Errorf("failed to create claim request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) "+
		"AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 "+
		"MicroMessenger/8.0.38(0x1800262c) NetType/4G Language/zh_CN")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Cookie", c.Config.Cookie)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		c.sendToBark("Follow: Error: " + err.Error())
		return "", fmt.Errorf("failed to send claim request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return "", fmt.Errorf("claim failed with status %d and error: %v", resp.StatusCode, err)
		}
		message := fmt.Sprintf("Claim Points Failed: %v", result["message"])
		c.sendToBark(message)
		return "", errors.New(message)
	}

	message := "Claim Points Success"
	c.sendToBark(message)
	return message, nil
}

// getCoinBalance retrieves the coin balance
func (c *Client) getCoinBalance() (string, error) {
	url := fmt.Sprintf("https://api.follow.is/wallets?userId=%s", c.Config.UserID)
	req, err := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create wallet request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) "+
		"AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 "+
		"MicroMessenger/8.0.38(0x1800262c) NetType/4G Language/zh_CN")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Cookie", c.Config.Cookie)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send wallet request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return "", fmt.Errorf("failed to decode wallet error response: %w", err)
		}
		return "", fmt.Errorf("failed to get wallet data: %v", result["message"])
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("error decoding wallet response: %w", err)
	}

	data, ok := result["data"].([]interface{})
	if !ok || len(data) == 0 {
		return "", errors.New("no wallet data found")
	}

	wallet, ok := data[0].(map[string]interface{})
	if !ok {
		return "", errors.New("invalid wallet data format")
	}

	dailyPowerTokenStr, ok := wallet["dailyPowerToken"].(string)
	if !ok {
		return "", errors.New("dailyPowerToken not found or invalid")
	}

	dailyPowerToken, err := strconv.ParseFloat(dailyPowerTokenStr, 64)
	if err != nil {
		return "", fmt.Errorf("failed to parse dailyPowerToken: %w", err)
	}

	// 将 dailyPowerToken 除以 10 的 18 次方
	dailyPowerToken /= 1e18

	return fmt.Sprintf("%f", dailyPowerToken), nil
}

func main() {
	// 初始化配置
	config := &Config{
		Cookie:         os.Getenv("COOKIE"),
		UserID:         os.Getenv("USER_ID"),
		BarkURL:        os.Getenv("BARK_URL"),
		TelegramToken:  os.Getenv("TELEGRAM_TOKEN"),
		TelegramChatID: os.Getenv("TELEGRAM_CHAT_ID"),
		ScheduledTime:  os.Getenv("SCHEDULED_TIME"),
		TimeZoneOffset: func() int {
			// TIMEZONE_OFFSET 可能不会配置，所以默认为东八区
			offsetStr := os.Getenv("TIMEZONE_OFFSET")
			if offsetStr == "" {
				log.Printf("TIMEZONE_OFFSET is not set, defaulting to 8")

			}

			offset, err := strconv.Atoi(offsetStr)
			if err != nil {
				log.Fatalf("Invalid TIMEZONE_OFFSET: %v", err)
				return 8
			}
			return offset
		}(),
	}

	// 验证必要的环境变量
	if config.Cookie == "" {
		log.Fatal("COOKIE must be set in environment variables")
	}
	if config.UserID == "" {
		log.Fatal("USER_ID must be set in environment variables")
	}

	// 设置默认调度时间
	if config.ScheduledTime == "" {
		config.ScheduledTime = "00:05"
	}

	hour, minute, err := parseScheduledTime(config.ScheduledTime)
	if err != nil {
		log.Fatalf("Invalid SCHEDULED_TIME format: %v", err)
	}

	client := NewClient(config)

	// 初始化 cron 调度器
	c := cron.New(cron.WithLocation(time.UTC))
	cronSpec := fmt.Sprintf("%d %d * * *", minute, hour) // 分 时 日 月 周

	_, err = c.AddFunc(cronSpec, func() {
		log.Println("Starting scheduled task...")

		claimResult, err := client.signFollow()
		if err != nil {
			log.Printf("signFollow error: %v", err)
			claimResult = err.Error()
		}

		balance, err := client.getCoinBalance()
		if err != nil {
			log.Printf("getCoinBalance error: %v", err)
			balance = err.Error()
		}

		currentDate := time.Now().In(time.FixedZone("UTC", config.TimeZoneOffset*60*60)).Format("2006-01-02 15:04:05") + " UTC" + fmt.Sprintf("%+d", config.TimeZoneOffset)
		logMessage := fmt.Sprintf("\n%s\nCoin Balance: %s\n\nDate: %s", claimResult, balance, currentDate)
		if config.TelegramToken != "" && config.TelegramChatID != "" {
			if err := client.sendToTelegram(logMessage); err != nil {
				log.Printf("Failed to send Telegram message: %v", err)
			} else {
				log.Println("Telegram message sent successfully.")
			}
		}

		log.Println("Scheduled task completed. \n", logMessage)
	})

	if err != nil {
		log.Fatalf("Error scheduling task: %v", err)
	}

	c.Start()
	log.Printf("Scheduler started. Will run daily at %02d:%02d UTC.\n", hour, minute)
	select {}
}
