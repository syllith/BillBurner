package example

import (
	"billburner/cd"
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

var browser context.Context
var closeBrowser context.CancelFunc

func main() {
	// Setup to handle termination signals to cleanly close the browser
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		closeBrowser()
		os.Exit(1)
	}()

	// Attempt to create a browser instance
	var err error
	browser, closeBrowser, err := cd.CreateBrowser(true, true, true)
	if err != nil {
		fmt.Println("Error creating browser:", err)
		return
	}
	defer closeBrowser()

	// Example of navigating to a website and performing actions
	navigateAndScrape(browser)
}

// navigateAndScrape demonstrates navigating to a website, inputting text, and scraping data
func navigateAndScrape(ctx context.Context) {
	// Navigate to example login page
	cd.Navigate(ctx, "https://example.com/login")

	// Input credentials
	cd.InputText(ctx, "#username", "yourUsername", false, true)
	cd.InputText(ctx, "#password", "yourPassword", false, true)

	// Click the login button
	cd.Click(ctx, "#loginButton", false)

	// Wait for navigation to complete
	cd.WaitForElement(ctx, "#dashboard")

	// Retrieve some data from the dashboard
	userData := cd.GetText(ctx, "#userData")
	fmt.Println("Retrieved User Data:", userData)

	// Example of taking a screenshot
	cd.CaptureScreenshot(ctx, "dashboard.png")
}
