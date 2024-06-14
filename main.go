package main

import (
	"billburner/cd"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/pterm/pterm"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
)

var browser context.Context
var closeBrowser context.CancelFunc

type Bill struct {
	amountDue float64
	dueDate   int64
	retrieved bool
}

func init() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}
}

func main() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		closeBrowser()
		os.Exit(1)
	}()

	go func() {
		time.Sleep(2 * time.Minute)
		closeBrowser()
		os.Exit(2)
	}()

	client := influxdb2.NewClient(os.Getenv("INFLUXDB_URL"), os.Getenv("INFLUXDB_TOKEN"))
	writeAPI := client.WriteAPIBlocking(os.Getenv("INFLUXDB_ORG"), os.Getenv("INFLUXDB_BUCKET"))
	defer client.Close()

	var err error
	browser, closeBrowser, err = cd.CreateBrowser(false, true, true)
	if err != nil {
		fmt.Println("Error creating browser:", err)
		return
	}
	defer closeBrowser()

	start := time.Now()

	var bills = []struct {
		name string
		bill *Bill
	}{
		{"Internet", &Bill{}},
		{"Wireless", &Bill{}},
		{"Water", &Bill{}},
		{"Power", &Bill{}},
		{"Gas", &Bill{}},
		{"Sewer", &Bill{}},
		{"Mortgage", &Bill{}},
		{"Car", &Bill{}},
		{"Insurance", &Bill{}},
	}

	// Retrieve bills in parallel
	for _, entry := range bills {
		switch entry.name {
		case "Car":
			getCarBill(entry.bill)
		case "Insurance":
			getInsuranceBill(entry.bill)
		case "Power":
			getPowerBill(entry.bill)
		case "Gas":
			getGasBill(entry.bill)
		case "Wireless":
			getPhoneBill(entry.bill, bills[0].bill)
			writeBillToInfluxDB(writeAPI, "Internet", bills[0].bill)
		case "Internet":
			continue // Handled with Wireless
		case "Sewer":
			getSewerBill(entry.bill)
		case "Water":
			getWaterBill(entry.bill)
		case "Mortgage":
			getMortgageBill(entry.bill)
		}

		//fmt.Println("\033[H\033[2J")
		renderBillTable(bills)

		if entry.bill.retrieved {
			writeBillToInfluxDB(writeAPI, entry.name, entry.bill)
		}
	}

	fmt.Println("Done :)")
	fmt.Println("Time Elapsed: ", time.Since(start))
}

func renderBillTable(bills []struct {
	name string
	bill *Bill
}) {
	rows := make([][]string, len(bills)+2) // +2 to account for the header and total row
	rows[0] = []string{"Bill Type", "Amount Due ($)", "Due Date", "Days Until Due"}
	totalDue := 0.0 // Initialize total amount due

	for i, entry := range bills {
		dueDate := "N/A"
		daysUntilDue := "N/A"
		if entry.bill.dueDate != 0 {
			dueTime := time.Unix(entry.bill.dueDate, 0)
			dueDate = dueTime.Format("01/02/2006")
			days := int(time.Until(dueTime).Hours() / 24)
			if days < -100 {
				daysUntilDue = "0"
			} else {
				daysUntilDue = strconv.Itoa(days)
			}
		}
		rows[i+1] = []string{entry.name, fmt.Sprintf("%.2f", entry.bill.amountDue), dueDate, daysUntilDue}
		totalDue += entry.bill.amountDue // Update the total amount due
	}

	// Add the total row
	rows[len(bills)+1] = []string{"Total", fmt.Sprintf("%.2f", totalDue), "", ""}

	pterm.DefaultTable.WithHasHeader(true).WithData(rows).Render()
}

func getCarBill(carBill *Bill) {
	carBill.amountDue = 422.94
	// Due date is always the 17th of the month. Factor in the current month to get the right due date. The due date should always be the following month, unless the date is after the 17th
	carBill.dueDate = time.Date(time.Now().Year(), time.Now().Month(), 17, 0, 0, 0, 0, time.Local).AddDate(0, 1, 0).Unix()
	carBill.retrieved = true
}

func getMortgageBill(mortgageBill *Bill) {
	const timeout = 15000 // milliseconds

	//* Navigate to login page
	cd.Navigate(browser, "https://mypennymac.pennymac.com/account/login")

	if !cd.ElementExists(browser, "#username", timeout) {
		log.Printf("error: username input not found within %d ms", timeout)
		return
	}

	//* Enter credentials
	cd.InputText(browser, "#username", "sinarian", false, false)
	cd.InputText(browser, "#password", "wr&a7PTBf!fE4#A", false, false)

	//* Click login button
	cd.Click(browser, "#submit-button", false)

	//* Wait for the email verification
	time.Sleep(10 * time.Second) // Consider reducing fixed sleep time or replacing it with a more dynamic wait if possible

	//* Enter code
	code := cd.GetCodeFromImap("hmail.digi-safe.co", os.Getenv("IMAP_USERNAME"), os.Getenv("IMAP_PASSWORD"), "Pennymac - Email Confirmation", `PM-`, "\n", false)
	cd.InputText(browser, "#tfaEmail", code, false, true)

	//* Click verify button
	cd.Click(browser, "#login-tfa-email-verify-btn", false)
	if !cd.ElementExists(browser, "div.r-edyy15:nth-child(1) > div:nth-child(1) > div:nth-child(1) > div:nth-child(1)", timeout) {
		log.Printf("error: verification section not found within %d ms", timeout)
		return
	}

	//* Get balance due
	balanceDue := cd.GetText(browser, "div.r-edyy15:nth-child(1) > div:nth-child(1) > div:nth-child(1) > div:nth-child(1)")
	mortgageBill.amountDue = stringToFloat(balanceDue)

	//* Get due date
	dueDate := cd.GetText(browser, "div.r-edyy15:nth-child(1) > div:nth-child(1) > div:nth-child(3) > div:nth-child(1)")
	mortgageBill.dueDate = extractWaterBillDueDate(dueDate)

	//* Mark as successfully retrieved
	mortgageBill.retrieved = true
}

func getPhoneBill(wirelessBill *Bill, internetBill *Bill) {
	//* Navigate to login page
	cd.Navigate(browser, "https://www.att.com/acctmgmt/signin")
	if !cd.ElementExists(browser, "#userID", 10000) {
		log.Printf("error: username input not found within 10s")
		return
	}

	time.Sleep(2 * time.Second)

	//* Enter username
	cd.InputText(browser, "#userID", os.Getenv("ATT_USERNAME"), true, true)
	time.Sleep(1 * time.Second)

	cd.Click(browser, "#continueFromUserLogin", false)
	if !cd.ElementExists(browser, "#password", 10000) {
		log.Printf("error: password input not found within 10s")
		return
	}
	time.Sleep(1 * time.Second)

	//* Enter password
	cd.InputText(browser, "#password", os.Getenv("ATT_PASSWORD"), true, true)

	time.Sleep(1 * time.Second)

	//* Click signin button
	cd.Click(browser, "#signin", false)

	if !cd.ElementExists(browser, "#chooseMethodMakePaymentButton", 10000) {
		log.Printf("error: make payment button not found within 10s")
		return
	}
	time.Sleep(1 * time.Second)

	//* Click make payment button
	cd.Click(browser, "#chooseMethodMakePaymentButton", false)
	if !cd.ElementExists(browser, ".page-title", 10000) {
		log.Printf("error: page title not found within 10s")
		return
	}
	time.Sleep(2 * time.Second)

	//* Wireless balance
	wirelessBalance := cd.GetText(browser, ".w-100")
	wirelessBill.amountDue = stringToFloat(wirelessBalance)

	//* Wireless due date
	wirelessBalanceDue := cd.GetText(browser, "div.fastpay-auth-page .option_date-picker .heading-xs")
	// Sample: Due Apr 28, 2024
	wirelessBill.dueDate = extractWirelessBillDueDate(wirelessBalanceDue)

	//* Click on internet tab
	cd.Click(browser, "div.jsx-2552546055:nth-child(1) > div:nth-child(1) > div:nth-child(3) > div:nth-child(1)", true)

	time.Sleep(2 * time.Second)

	//* Internet balance
	internetBalance := cd.GetText(browser, ".w-100")
	internetBill.amountDue = stringToFloat(internetBalance)

	//* Internet due date
	internetBalanceDue := cd.GetText(browser, "div.jsx-3631953385:nth-child(3)")
	// Sample: Due Apr 28, 2024
	internetBill.dueDate = extractInternetBillDueDate(internetBalanceDue)
	internetBill.retrieved = true
	wirelessBill.retrieved = true
}

func getInsuranceBill(insuranceBill *Bill) {
	//* Navigate to login page
	cd.Navigate(browser, "https://proofing.statefarm.com/login-ui/login")
	if !cd.ElementExists(browser, "#username", 10000) {
		log.Printf("error: username input not found within 10s")
		return
	}

	//* Enter credentials
	cd.InputText(browser, "#username", os.Getenv("STATE_FARM_USERNAME"), false, false)
	cd.InputText(browser, "#password", os.Getenv("STATE_FARM_PASSWORD"), false, false)

	//* Click login button
	cd.Click(browser, "#submitButton", true)
	if !cd.ElementExists(browser, "#emailAddress > label:nth-child(2)", 10000) {
		log.Printf("error: email verification not found within 10s")
		return
	}

	//* Click email verification
	cd.Click(browser, "#emailAddress > label:nth-child(2)", true)
	cd.Click(browser, "#submitButton", true)

	time.Sleep(10 * time.Second)

	//* Enter code
	code := cd.GetCodeFromImap("hmail.digi-safe.co", os.Getenv("IMAP_USERNAME"), os.Getenv("IMAP_PASSWORD"), "Verification Code", `<span style=3D"color:#E22925;">`, "</", false)

	cd.InputText(browser, "#verification_code", code, false, false)
	cd.Click(browser, "#submitButton", true)
	if !cd.ElementExists(browser, ".bill-due-amt-txt", 10000) {
		log.Printf("error: balance due not found within 10s")
		return
	}

	//* Balance due
	balanceDue := cd.GetText(browser, ".bill-due-amt-txt")
	insuranceBill.amountDue = stringToFloat(balanceDue)

	//* Due date
	dueDate := cd.GetText(browser, ".bill-due-date")
	// Sample: May 17
	insuranceBill.dueDate = extractInsuranceBillDueDate(dueDate)
	insuranceBill.retrieved = true
}

func getGasBill(gasBill *Bill) {
	//* Navigate to login page
	cd.Navigate(browser, "https://myaccount.spireenergy.com/web/customer/registration/#/sign-in")
	if !cd.ElementExists(browser, "#loginEmail", 10000) {
		log.Printf("error: username input not found within 10s")
		return
	}

	//* Enter credentials
	cd.InputText(browser, "#loginEmail", os.Getenv("SPIRE_USERNAME"), false, false)
	cd.InputText(browser, "#loginPassword", os.Getenv("SPIRE_PASSWORD"), false, false)

	//* Click login button
	cd.Click(browser, "section.buttons:nth-child(4) > button:nth-child(1)", false)
	if !cd.ElementExists(browser, ".amount-due", 10000) {
		log.Printf("error: balance due not found within 10s")
		return
	}

	time.Sleep(1 * time.Second)

	//* Balance due
	balanceDue := cd.GetText(browser, ".amount-due")
	gasBill.amountDue = stringToFloat(balanceDue)

	//* Due date
	balanceDue = cd.GetText(browser, ".due-date")
	// Sample: May 08, 2024
	gasBill.dueDate = extractGasBillDueDate(balanceDue)
	gasBill.retrieved = true
}

func getSewerBill(sewerBill *Bill) {
	//* Navigate to login page
	cd.Navigate(browser, "https://myaccount.stlmsd.com/MSDSSP/Index.aspx")
	if !cd.ElementExists(browser, "#body_content_txtUsername", 10000) {
		log.Printf("error: username input not found within 10s")
		return
	}

	//* Enter credentials
	cd.InputText(browser, "#body_content_txtUsername", os.Getenv("STLMSD_USERNAME"), true, false)
	cd.InputText(browser, "#body_content_txtPassword", os.Getenv("STLMSD_PASSWORD"), true, false)

	//* Click login button
	cd.Click(browser, "#body_content_btnLogin", false)
	if !cd.ElementExists(browser, "#body_content_AccountSummaryTabControl_BillingSummaryControl1_lblCurrentBalanceText", 10000) {
		log.Printf("error: balance due not found within 10s")
		return
	}

	//* Balance due
	balanceDue := cd.GetText(browser, "#body_content_AccountSummaryTabControl_BillingSummaryControl1_lblCurrentBalanceText")
	sewerBill.amountDue = stringToFloat(balanceDue)

	//* Due date
	balanceDue = cd.GetText(browser, "#body_content_AccountSummaryTabControl_BillingSummaryControl1_lblAppOrLatePaymentDateText")
	// Sample: May 6, 2024
	sewerBill.dueDate = extractSewerBillDueDate(balanceDue)
	sewerBill.retrieved = true
}

func getPowerBill(powerBill *Bill) {
	//* Navigate to the login page
	cd.Navigate(browser, "https://www.ameren.com/login-page/")
	if !cd.ElementExists(browser, "#txtSignInEmail", 10000) {
		log.Printf("error: email input not found within 10s")
		return
	}

	//* Enter credentials
	cd.InputText(browser, "#txtSignInEmail", os.Getenv("AMEREN_USERNAME"), false, false)
	cd.InputText(browser, ".input-password > input:nth-child(1)", os.Getenv("AMEREN_PASSWORD"), false, false)

	//* Click the login button
	cd.Click(browser, "#btnLogin", false)
	if !cd.ElementExists(browser, ".amount", 10000) {
		log.Printf("error: balance due not found within 10s")
		return
	}

	time.Sleep(2 * time.Second)

	//* Balance due
	amountDue := cd.GetText(browser, ".amount")

	//* Due date
	dueDate := cd.GetText(browser, ".alert")

	powerBill.amountDue = stringToFloat(amountDue)
	powerBill.dueDate = extractPowerBillDueDate(dueDate)
	powerBill.retrieved = true
}

func getWaterBill(waterBill *Bill) {
	//* Navigate to login page
	cd.Navigate(browser, "https://stlo-egov.aspgov.com/Click2GovCX/index.html")
	if !cd.ElementExists(browser, ".lastTopRowMenuItem > a:nth-child(1)", 10000) {
		log.Printf("error: login button not found within 10s")
		return
	}

	//* Click login button
	cd.Click(browser, ".lastTopRowMenuItem > a:nth-child(1)", false)
	if !cd.ElementExists(browser, "#email\\.emailId", 10000) {
		log.Printf("error: username input not found within 10s")
		return
	}

	//* Enter credentials
	cd.InputText(browser, "#email\\.emailId", os.Getenv("STLO_EGOV_USERNAME"), true, false)
	cd.InputText(browser, "#password", os.Getenv("STLO_EGOV_PASSWORD"), true, false)

	//* Click logon button
	cd.Click(browser, "#submitButton", false)
	if !cd.ElementExists(browser, ".menuWrapper > ul:nth-child(1) > li:nth-child(6) > a:nth-child(1)", 10000) {
		log.Printf("error: account info button not found within 10s")
		return
	}

	//* Click account info
	cd.Click(browser, ".menuWrapper > ul:nth-child(1) > li:nth-child(6) > a:nth-child(1)", false)
	if !cd.ElementExists(browser, ".menuWrapper > ul:nth-child(1) > li:nth-child(6) > a:nth-child(1)", 10000) {
		log.Printf("error: balance due not found within 10s")
		return
	}

	//* Get balance due
	balanceDue := cd.GetText(browser, ".menuWrapper > ul:nth-child(1) > li:nth-child(6) > a:nth-child(1)")
	waterBill.amountDue = stringToFloat(balanceDue)

	//* Get due date
	dueDate := cd.GetText(browser, "#contentPanel > p:nth-child(9)")
	dueDate = strings.Split(dueDate, "due on ")[1]
	dueDate = strings.Split(dueDate, ".")[0]
	// Sample: 04/23/2024
	waterBill.dueDate = extractWaterBillDueDate(dueDate)
	waterBill.retrieved = true
}

// Helper function to convert a string to a float like we are doing in the rest of the code with regex
func stringToFloat(value string) float64 {
	re := regexp.MustCompile(`\$\s*([0-9]+\.[0-9]+)`)
	match := re.FindStringSubmatch(value)

	if len(match) > 1 {
		numberStr := match[1]

		// Convert string to float
		balance, err := strconv.ParseFloat(numberStr, 64)
		if err != nil {
			return 0
		}

		return balance
	} else {
		return 0
	}
}

// Helper function to parse date from string and return Unix time

func parseDate(dateStr, format string) int64 {
	t, err := time.Parse(format, dateStr)
	if err != nil {
		return 0
	}
	// Add 1 day to the parsed time
	t = t.AddDate(0, 0, 1)
	return t.Unix()
}

func extractPowerBillDueDate(input string) int64 {
	format := "01/02/06"
	parts := strings.Split(input, "by")
	if len(parts) < 2 {
		return 0
	}
	datePart := strings.TrimSpace(parts[1])
	dateStr := strings.Fields(datePart)[0]
	if dateStr == "" {
		return 0
	}
	return parseDate(dateStr, format)
}

func extractGasBillDueDate(input string) int64 {
	format := "Jan 02, 2006"
	return parseDate(input, format)
}

func extractWirelessBillDueDate(input string) int64 {
	format := "Due Jan 2, 2006"
	return parseDate(input, format)
}

func extractInternetBillDueDate(input string) int64 {
	format := "Due Jan 2, 2006"
	return parseDate(input, format)
}

func extractInsuranceBillDueDate(input string) int64 {
	cleanedInput := strings.TrimSpace(input)
	cleanedInput = strings.Replace(cleanedInput, "\n", " ", -1)
	cleanedInput = strings.Replace(cleanedInput, "Due date", "", -1)
	cleanedInput = strings.TrimSpace(cleanedInput)
	currentYear := time.Now().Year()
	dateWithYear := fmt.Sprintf("%s %d", cleanedInput, currentYear)
	format := "Jan 2 2006"
	t, err := time.Parse(format, dateWithYear)
	if err != nil {
		fmt.Println("Error parsing date:", err)
		return 0
	}
	t = t.AddDate(0, 0, 1)
	return t.Unix()
}

func extractSewerBillDueDate(input string) int64 {
	format := "Jan 2, 2006"
	return parseDate(input, format)
}

func extractWaterBillDueDate(input string) int64 {
	format := "01/02/2006"
	return parseDate(input, format)
}

func writeBillToInfluxDB(writeAPI api.WriteAPIBlocking, billName string, bill *Bill) {
	daysUntilDue := int(time.Until(time.Unix(bill.dueDate, 0)).Hours() / 24)
	if daysUntilDue < -100 {
		daysUntilDue = 0
	}

	point := influxdb2.NewPointWithMeasurement("bill").
		AddTag("type", billName).
		AddField("amount_due", bill.amountDue).
		AddField("due_date", time.Unix(bill.dueDate, 0).UTC().Format(time.RFC3339)). // Format as ISO 8601
		AddField("days_until_due", daysUntilDue).
		SetTime(time.Now())

	if err := writeAPI.WritePoint(context.Background(), point); err != nil {
		fmt.Printf("Error writing point to InfluxDB: %s\n", err)
	}
}
