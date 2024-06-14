package cd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

var bypassScript = `(function(w, n, wn) {
	Object.defineProperty(n, 'webdriver', {
		get: () => false,
	});

	Object.defineProperty(n, 'plugins', {
		get: () => [1, 2, 3, 4, 5],
	});

	Object.defineProperty(n, 'languages', {
		get: () => ['en-US', 'en'],
	});

	w.chrome = {
		runtime: {},
	};

	const originalQuery = wn.permissions.query;
	return wn.permissions.query = (parameters) => (
		parameters.name === 'notifications' ?
		Promise.resolve({
			state: Notification.permission
		}) :
		originalQuery(parameters)
	);
})(window, navigator, window.navigator);`

// Creates a single new browser instance, returned in 2 parts. The first part is the context, which manages the browser actions and states and must be passed to any function used in this package. The second part is a cancel function, which can be used to close the browser instance.
//
// - Headless determines if the browser window is visible or not.
//
// - Fresh determines if the browser should start with a clean profile.
//
// - EnableBypass determines if the browser should bypass potential bot detection.
func CreateBrowser(headless, fresh, enableBypass bool) (context.Context, context.CancelFunc, error) {
	if !fileExists(roamingDir() + "/ChromeDriver") {
		err := os.Mkdir(roamingDir()+"/ChromeDriver", 0755)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating Chrome data directory: %v", err)
		}
	}

	if fresh && fileExists(roamingDir()+"/ChromeDriver/Profile") {
		err := os.RemoveAll(roamingDir() + "/ChromeDriver/Profile")
		if err != nil {
			return nil, nil, fmt.Errorf("error removing existing Chrome data directory: %v", err)
		}
	}

	opts := []chromedp.ExecAllocatorOption{
		chromedp.WindowSize(1280, 850),
		chromedp.UserDataDir(roamingDir() + "/ChromeDriver/Profile"),
		chromedp.Flag("profile-directory", "Profile"),
	}

	if headless {
		opts = append(opts, chromedp.Headless)
	}

	// Create a new execution context with the specified options.
	allocCtx, _ := chromedp.NewExecAllocator(context.Background(), opts...)

	// Create a new Chromedp context using the allocator context
	ctx, closeBrowser := chromedp.NewContext(allocCtx)

	if enableBypass {
		// Execute scripts on the new document to bypass potential limitations.
		err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(bypassScript).Do(ctx)
			return err
		}))
		if err != nil {
			return nil, nil, fmt.Errorf("error adding bypass script: %v", err)
		}
	}

	Navigate(ctx, "about:blank")

	return ctx, closeBrowser, nil
}

// InputText sets text on an input element and optionally triggers input-related events.
//
// - selector specifies the CSS selector of the input element to target.
//
// - input is the string value to be set on the targeted input element.
//
//   - useEval determines the method to set the value:
//     true uses JavaScript evaluation to directly set the element's value.
//     false uses chromedp.SendKeys to simulate typing into the input field.
//
//   - useTrip determines if input and change events should be dispatched after setting the value:
//     true triggers both 'input' and 'change' events to simulate more natural user interaction.
//     false sets the value without triggering these events.
//
// The function handles errors that may occur during the chromedp action execution and returns them.
func InputText(ctx context.Context, selector string, input string, useEval bool, useTrip bool) {
	var actions []chromedp.Action

	if useEval {
		setValueJS := fmt.Sprintf(`document.querySelector(%q).value = %q;`, selector, input)
		actions = append(actions, chromedp.Evaluate(setValueJS, nil))
	} else {
		actions = append(actions, chromedp.SendKeys(selector, input))
	}

	if useTrip {
		time.Sleep(500 * time.Millisecond)
		dispatchInputJS := fmt.Sprintf(`document.querySelector(%q).dispatchEvent(new Event('input', { bubbles: true }));`, selector)
		dispatchChangeJS := fmt.Sprintf(`document.querySelector(%q).dispatchEvent(new Event('change', { bubbles: true }));`, selector)
		actions = append(actions, chromedp.Evaluate(dispatchInputJS, nil), chromedp.Evaluate(dispatchChangeJS, nil))
	}

	if err := chromedp.Run(ctx, actions...); err != nil {
		log.Printf("failed to input text: %v", err)
	}

}

// GetAttribute retrieves an attribute from a DOM element specified by a CSS selector.
//
// - ctx is the Chromedp context which manages the underlying browser actions and states.
//
// - attribute specifies the name of the attribute to be retrieved from the targeted element.
//
// - selector is the CSS selector used to locate the element from which the attribute should be retrieved.
//
//   - useEval determines the method of attribute retrieval:
//     true uses JavaScript evaluation to fetch the attribute, which allows accessing dynamically set attributes.
//     false uses the standard Chromedp method to fetch the attribute value, typically used for statically available attributes.
//
// The function returns the attribute value as a string and an error if any issues occur during execution.
func GetAttribute(ctx context.Context, attribute string, selector string, useEval bool) string {
	var res string
	var action chromedp.Action

	if useEval {
		// Use JavaScript evaluation to fetch attribute
		jsScript := fmt.Sprintf(`document.querySelector(`+"`%s`"+`).getAttribute(`+"`%s`"+`)`, selector, attribute)
		action = chromedp.Evaluate(jsScript, &res)
	} else {
		// Use AttributeValue to fetch the attribute directly
		var present bool
		action = chromedp.AttributeValue(selector, attribute, &res, &present, chromedp.AtLeast(0))
	}

	// Execute the appropriate chromedp action
	if err := chromedp.Run(ctx, action); err != nil {
		log.Printf("failed to get attribute %q from selector %q: %v", attribute, selector, err)
		return ""
	}

	return res
}

// GetText retrieves the text content from a DOM element specified by a CSS selector.
//
// - ctx is the Chromedp context which manages the underlying browser actions and states.
//
// - selector is the CSS selector used to locate the element from which the text should be retrieved.
//
// The function returns the text content as a string and an error if any issues occur during execution.
func GetText(ctx context.Context, selector string) string {
	var value string
	if err := chromedp.Run(ctx, chromedp.Text(selector, &value, chromedp.AtLeast(0))); err != nil {
		log.Printf("failed to get text from selector %q: %v", selector, err)
		return ""
	}
	return value
}

// Click performs a click action on a DOM element specified by a CSS selector.
// It can execute the click either directly or through JavaScript evaluation based on the useEval flag.
//
// - ctx is the Chromedp context which manages the underlying browser actions and states.
//
// - selector is the CSS selector used to locate the element to be clicked.
//
//   - useEval determines the method of executing the click:
//     true uses JavaScript evaluation to trigger the click, which can bypass certain DOM event listeners.
//     false uses the standard Chromedp click action, which simulates a more realistic user interaction.
//
// Errors during execution are logged, not returned.
func Click(ctx context.Context, selector string, useEval bool) {
	var err error
	if useEval {
		// Perform click using JavaScript
		jsClick := fmt.Sprintf(`document.querySelector("%s").click()`, selector)
		err = chromedp.Run(ctx, chromedp.Evaluate(jsClick, nil))
	} else {
		// Perform click using chromedp's built-in function
		err = chromedp.Run(ctx, chromedp.Click(selector, chromedp.NodeVisible))
	}

	if err != nil {
		log.Printf("failed to click on selector %q: %v", selector, err)
	}
}

// SetClass sets the class attribute of a DOM element specified by a CSS selector.
//
// - ctx is the Chromedp context which manages the underlying browser actions and states.
//
// - selector is the CSS selector used to locate the element for which the class attribute is to be set.
//
// - newClasses is the new class string to be applied to the targeted element.
//
// Errors during execution are logged, not returned. This function directly manipulates the class attribute using JavaScript.
func SetClass(ctx context.Context, selector string, newClasses string) {
	code := fmt.Sprintf(`document.querySelector("%s").className = "%s"`, selector, newClasses)
	if err := chromedp.Run(ctx, chromedp.Evaluate(code, nil)); err != nil {
		log.Printf("error setting class for selector %q: %v", selector, err)
	}
}

// GetNodes retrieves all DOM nodes matching a specified CSS selector.
//
// - ctx is the Chromedp context which manages the underlying browser actions and states.
//
// - selector is the CSS selector used to locate the elements from which nodes are to be retrieved.
//
// The function logs any errors that occur during execution and returns a slice of pointers to the cdp.Node objects.
func GetNodes(ctx context.Context, selector string) []*cdp.Node {
	var nodes []*cdp.Node
	if err := chromedp.Run(ctx, chromedp.Nodes(selector, &nodes, chromedp.ByQueryAll)); err != nil {
		log.Printf("error retrieving nodes for selector %q: %v", selector, err)
		return nil
	}
	return nodes
}

// Navigate directs the browser to a specified URL.
//
// - ctx is the Chromedp context which manages the underlying browser actions and states.
//
// - url is the web address to which the browser should navigate.
//
// This function logs any errors that occur during the navigation process.
func Navigate(ctx context.Context, url string) {
	if err := chromedp.Run(ctx, chromedp.Navigate(url)); err != nil {
		log.Printf("error navigating to URL %q: %v", url, err)
	}
}

// WaitForElement waits until the specified DOM element is ready in the page. This code will block indefinitely until the element is ready or an error occurs. Use ElementExists for a more flexible approach with a timeout period.
//
// - ctx is the Chromedp context which manages the underlying browser actions and states.
//
// - selector is the CSS selector of the element to wait for.
//
// Errors during execution are logged.
func WaitForElement(ctx context.Context, selector string) {
	if err := chromedp.Run(ctx, chromedp.WaitReady(selector)); err != nil {
		log.Printf("error waiting for element %q to be ready: %v", selector, err)
	}
}

// ElementExists checks for the existence of a DOM element specified by a CSS selector within a timeout period.
//
// - ctx is the Chromedp context which manages the underlying browser actions and states.
//
// - selector is the CSS selector used to locate the element.
//
// - timeout is the maximum time in milliseconds to wait for the element to appear.
//
// Returns true if the element appears within the timeout, otherwise false. Errors during the process are logged.
func ElementExists(ctx context.Context, selector string, timeout int64) bool {
	st := time.Now()

	for {
		tctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
		defer cancel()

		var nodes []*cdp.Node
		if err := chromedp.Run(tctx, chromedp.Nodes(selector, &nodes, chromedp.ByQueryAll)); err != nil {
			log.Printf("error checking existence for selector %q: %v", selector, err)
		}

		if len(nodes) > 0 {
			return true
		}

		if time.Since(st).Milliseconds() > timeout {
			return false
		}
		time.Sleep(250 * time.Millisecond) // Pause briefly to avoid hammering the CPU
	}
}

// Reload refreshes the current page.
//
// - ctx is the Chromedp context which manages the underlying browser actions and states.
//
// Errors during execution are logged.
func Reload(ctx context.Context) {
	if err := chromedp.Run(ctx, chromedp.Reload()); err != nil {
		log.Printf("error reloading the page: %v", err)
	}
}

// GetSource retrieves the outer HTML of the entire document.
//
// - ctx is the Chromedp context which manages the underlying browser actions and states.
//
// Returns the HTML source as a string. Errors during execution are logged.
func GetSource(ctx context.Context) string {
	var res string
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		node, err := dom.GetDocument().Do(ctx)
		if err != nil {
			return err
		}
		res, err = dom.GetOuterHTML().WithNodeID(node.NodeID).Do(ctx)
		return err
	})); err != nil {
		log.Printf("error retrieving the page source: %v", err)
		return ""
	}
	return res
}

// SubmitForm submits a form identified by a CSS selector.
//
// - ctx is the Chromedp context which manages the underlying browser actions and states.
//
// - formSelector is the CSS selector of the form to submit.
//
// Errors during execution are logged.
func SubmitForm(ctx context.Context, formSelector string) {
	code := fmt.Sprintf(`document.querySelector("%s").submit()`, formSelector)
	if err := chromedp.Run(ctx, chromedp.Evaluate(code, nil)); err != nil {
		log.Printf("error submitting form %q: %v", formSelector, err)
	}
}

// RunEval executes JavaScript code in the browser context and logs any errors.
//
// - ctx is the Chromedp context which manages the underlying browser actions and states.
//
// - eval is the JavaScript code to be executed.
//
// Errors during execution are logged, not returned.
func RunEval(ctx context.Context, eval string) {
	var res string
	if err := chromedp.Run(ctx, chromedp.Evaluate(eval, &res)); err != nil {
		log.Printf("error executing JavaScript: %v", err)
	}
}

// GetURL retrieves the current page URL from the browser.
//
// - ctx is the Chromedp context which manages the underlying browser actions and states.
//
// Returns the URL as a string. Errors during URL retrieval are logged.
func GetURL(ctx context.Context) string {
	var value string
	if err := chromedp.Run(ctx, chromedp.Location(&value)); err != nil {
		log.Printf("error retrieving current URL: %v", err)
		return ""
	}
	return value
}

// WaitForUrlChange blocks until the current URL changes from the specified URL.
//
// - ctx is the Chromedp context which manages the underlying browser actions and states.
//
// - currentUrl is the URL to be checked against the current URL.
//
// This function logs errors during URL retrieval and uses a delay to mitigate high CPU usage.
func WaitForUrlChange(ctx context.Context, currentUrl string) {
	for {
		if url := GetURL(ctx); url != currentUrl {
			if url == "" { // If error occurred during GetURL, it returns empty string
				continue // Skip iteration to prevent false positive exit if URL couldn't be retrieved
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// CaptureScreenshot captures a screenshot of the current viewport and logs any errors.
//
// - ctx is the Chromedp context which manages the underlying browser actions and states.
//
// - filename is the name of the file to save the screenshot to.
func CaptureScreenshot(ctx context.Context, filename string) {
	var buf []byte
	if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&buf)); err != nil {
		log.Printf("failed to capture screenshot: %v", err)
		return
	}
	if err := os.WriteFile(filename, buf, 0644); err != nil {
		log.Printf("failed to save screenshot: %v", err)
	}
}

// Wait pauses the current goroutine for the specified duration in milliseconds.
func Wait(durationMs int) {
	time.Sleep(time.Duration(durationMs) * time.Millisecond)
}

func roamingDir() string {
	roaming, _ := os.UserConfigDir()
	return roaming
}

func fileExists(path string) bool {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false
	}
	return true
}

// GetCodeFromImap retrieves a verification code from an IMAP server based on the provided parameters.
//
// It logs into the server using the provided email credentials, searches the INBOX for the latest email with the specified subject, and extracts a code from the email body using the specified delimiters.
// - emailServer is the host and optionally the port of the IMAP server.
//
// - emailAddress is the email address used for login.
//
// - password is the password used for login.
//
// - emailSubject is the subject line to search for within the mailbox.
//
// - delimPart1 and delimPart2 are delimiters used to extract the code from the email body.
//
// - useTLS specifies whether to use TLS (secure) or not.
func GetCodeFromImap(emailServer, emailAddress, password, emailSubject, delimPart1, delimPart2 string, useTLS bool) string {
	var c *client.Client
	var err error

	// Connect to the server with or without TLS
	if useTLS {
		c, err = client.DialTLS(emailServer+":993", nil)
	} else {
		c, err = client.Dial(emailServer + ":143")
	}
	if err != nil {
		log.Printf("error connecting to IMAP server: %v", err)
		return ""
	}
	defer c.Logout()

	// Login with provided credentials
	err = c.Login(emailAddress, password)
	if err != nil {
		log.Printf("error logging into IMAP server: %v", err)
		return ""
	}

	// Select INBOX
	_, err = c.Select("INBOX", false)
	if err != nil {
		log.Printf("error selecting INBOX: %v", err)
		return ""
	}

	// Search for emails with the specified subject
	criteria := imap.NewSearchCriteria()
	criteria.Header.Add("Subject", emailSubject)
	ids, err := c.Search(criteria)
	if err != nil {
		log.Printf("error searching emails: %v", err)
		return ""
	}
	if len(ids) == 0 {
		log.Printf("no emails found with subject: %s", emailSubject)
		return "" // No emails found
	}

	// Get the most recent email with the specified subject
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(ids[len(ids)-1])

	var section imap.BodySectionName
	section.Peek = true
	messages := make(chan *imap.Message, 1)
	go func() {
		c.Fetch(seqSet, []imap.FetchItem{section.FetchItem()}, messages)
	}()

	// Read the message body
	msg := <-messages
	if msg == nil {
		log.Printf("no message found with subject: %s", emailSubject)
		return "" // No message found
	}
	r := msg.GetBody(&section)
	body, err := io.ReadAll(r)
	if err != nil {
		log.Printf("error reading email body: %v", err)
		return ""
	}

	// Split the body to find the verification code
	part1 := strings.Split(string(body), delimPart1)
	if len(part1) < 2 {
		log.Printf("delimiter %q not found in body", delimPart1)
		return ""
	}
	part2 := strings.Split(part1[1], delimPart2)
	if len(part2) < 1 {
		log.Printf("delimiter %q not found after first split", delimPart2)
		return ""
	}
	code := strings.TrimSpace(part2[0])

	return code
}

// SavePageSource retrieves the HTML source of the current page and saves it to a file named source.html in the current directory.
//
// - ctx is the Chromedp context which manages the underlying browser actions and states.
//
// This function logs any errors that occur during the source retrieval and file saving process.
func SavePageSource(ctx context.Context) {
	var res string
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		node, err := dom.GetDocument().Do(ctx)
		if err != nil {
			return err
		}
		res, err = dom.GetOuterHTML().WithNodeID(node.NodeID).Do(ctx)
		return err
	})); err != nil {
		log.Printf("error retrieving the page source: %v", err)
		return
	}

	// Save the source to a file
	filename := "source.html"
	if err := os.WriteFile(filename, []byte(res), 0644); err != nil {
		log.Printf("failed to save page source to %s: %v", filename, err)
	} else {
		log.Printf("page source saved to %s successfully", filename)
	}
}
