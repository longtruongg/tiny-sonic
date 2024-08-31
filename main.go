package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	_ "github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/robfig/cron/v3"
	"golang.org/x/time/rate"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	sonicRpc       = "https://devnet.sonic.game"
	claimBox       = "https://odyssey-api-beta.sonic.game/user/transactions/rewards/claim"
	sonicAuthor    = "https://odyssey-api-beta.sonic.game/auth/sonic/authorize"
	sonicChallegen = "https://odyssey-api-beta.sonic.game/auth/sonic/challenge?"
	sonicGetInfo   = "https://odyssey-api-beta.sonic.game/user/rewards/info"
	sonicCheckIn   = "https://odyssey-api-beta.sonic.game/user/check-in"
	sonicCheckinTx = "https://odyssey-api-beta.sonic.game/user/check-in/transaction"
	baseFile       = "base.json"
	dailyTransfer  = "https://odyssey-api-beta.sonic.game/user/transactions/state/daily"
)

var header = http.Header{
	"Content-Type":  []string{"application/json"},
	"Accept":        []string{"application/json"},
	"prama":         []string{"no-cache"},
	"Cache-Control": []string{"no-cache"},
	"referer": []string{
		"https://odyssey.sonic.game/",
	},
	"Priority": []string{`u=1, i`},
	"origin": []string{
		"https://odyssey.sonic.game/",
	},
	"accept-language": []string{`"en-us", "en", "q=0.9"`},
	"accept-encoding": []string{"gzip", "br", "deflate", "*/*", "zstd"},
	"cache-control":   []string{"no-cache"},
	"User-Agent": []string{
		`Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/39.0.2171.27 Safari/537.36`,
	},
}

type Player struct {
	Idx  string `json:"Idx"`
	Pub  string `json:"Pub"`
	Prik string `json:"Prik"`
}

type SonicProfile struct {
	Code int `json:"-,omitempty"`
	Data struct {
		WalletBalance int64 `json:"wallet_balance"`
		Ring          int   `json:"ring"`
		RingMonitor   int   `json:"ring_monitor"`
	} `json:"data"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type SonicToken struct {
	Code    int64  `json:"-,omitempty"`
	Data    Data   `json:"data"`
	Status  string `json:"-"`
	Message string `json:"message"`
}

type Data struct {
	Token string `json:"token"`
}

type Payload struct {
	PublicAddr   string `json:"address"`
	Signature    string `json:"signature"`
	PublicEncode string `json:"address_encoded"`
}
type CheckInPay struct {
	Code    int    `json:"-"`
	Message string `json:"message"`
	Status  string `json:"-"`
	Data    struct {
		Hash string `json:"hash"`
	} `json:"data"`
}
type DailyTransfer struct {
	Code int `json:"code"`
	Data struct {
		TotalTransactions int `json:"total_transactions"`
		StageInfo         struct {
			Stage1 struct {
				Claimed  bool `json:"claimed"`
				Rewards  int  `json:"rewards"`
				Quantity int  `json:"quantity"`
			} `json:"stage_1"`
			Stage2 struct {
				Claimed  bool `json:"claimed"`
				Rewards  int  `json:"rewards"`
				Quantity int  `json:"quantity"`
			} `json:"stage_2"`
			Stage3 struct {
				Claimed  bool `json:"claimed"`
				Rewards  int  `json:"rewards"`
				Quantity int  `json:"quantity"`
			} `json:"stage_3"`
		} `json:"stage_info"`
	} `json:"data"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type HashType struct {
	//Jsonrpc string   `json:"jsonrpc"`
	//ID      string    `json:"id"`
	//Method  string   `json:"method"`
	//Params  []string `json:"params"`
	Hash string `json:"hash"`
}

func main() {

	//
	couter := 0
	c := cron.New(cron.WithLogger(
		cron.DefaultLogger))
	solClient := rpc.NewWithCustomRPCClient(
		rpc.NewWithLimiter(
			sonicRpc,
			rate.Every(time.Second), // time frame
			5),
	)
	payer, _ := solana.PrivateKeyFromBase58("prik")
	val, _ := solClient.GetBalance(context.Background(), payer.PublicKey(), "")
	fmt.Println("Balance->", val.Value)
	playerArr, err := loadSonicPlayers()
	if err != nil {
		panic(fmt.Sprintf("cannot load players ->%v", err))
	}
	// TODO : fix after :shame:
	c.AddFunc("@every 3m", func() {
		for _, val := range playerArr {

			err := SonicTransfer(solClient, payer, solana.PublicKeyFromBytes([]byte(val.Pub)))
			if err != nil {
				panic(fmt.Sprintf("cannot excuted sonic ->%v", err))
			}
			couter++
		}
		if couter == 100 {
			fmt.Println("bingo ->", couter)
		}

	})
	c.Run()

}

func doTransaction(ctx context.Context, tx string, solClient *rpc.Client, payer solana.PrivateKey) (solana.Signature, error) {
	data, err := base64.StdEncoding.DecodeString(tx)
	if err != nil {
		return [64]byte{}, fmt.Errorf("cannot decodeTx ->%v", err)
	}
	val, err := solana.TransactionFromDecoder(bin.NewBinDecoder(data))
	if err != nil {
		return [64]byte{}, fmt.Errorf("cannot get Tx from decoder ->%v", err)
	}
	_, err = val.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if payer.PublicKey().Equals(key) {
			return &payer
		}
		return nil
	})
	if err != nil {
		return [64]byte{}, fmt.Errorf("cannot sign function ->%v", err)
	}
	opts := rpc.TransactionOpts{
		Encoding:            "base64",
		SkipPreflight:       true, // avoid find blockHash
		PreflightCommitment: "max",
		MaxRetries:          nil,
		MinContextSlot:      nil,
	}
	return solClient.SendTransactionWithOpts(
		ctx,
		val,
		opts,
	)
}

func requestSonic(ctx context.Context, client *http.Client, token, method, endpoint string, data []byte) ([]byte, error) {
	deadline, cancel := context.WithDeadline(ctx, time.Now().Add(time.Second*5))
	defer cancel()
	req, err := http.NewRequestWithContext(deadline, method, endpoint, bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("cannot create request ->%v", err)
	}
	for k, v := range header {
		req.Header.Add(k, v[0])
		if len(token) != 0 {
			req.Header.Add("Authorization", token)
		}
	}

	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot dorequest ->%v", err)
	}
	defer response.Body.Close()
	return io.ReadAll(response.Body)
}

func dailyCheckIn(ctx context.Context, client *http.Client, token string) (*CheckInPay, error) {
	response, err := requestSonic(ctx, client, token, http.MethodGet, sonicCheckinTx, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot send request: %w", err)
	}

	var checkIn CheckInPay
	if err = json.Unmarshal(response, &checkIn); err != nil {
		return nil, fmt.Errorf("cannot parse response: %w", err)
	}
	if checkIn.Status == "fail" {
		return nil, fmt.Errorf("%v", checkIn.Message)
	}

	return &checkIn, nil
}

type StageStep struct {
	Stage int `json:"stage"`
}
type BoxClaimed struct {
	Code int `json:"code,omitempty"`
	Data struct {
		Stage   int  `json:"stage"`
		Claimed bool `json:"claimed"`
	} `json:"data,omitempty"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func claimBoxMyth(client *http.Client, token string) (interface{}, error) {
	const maxStage = 3
	var stage StageStep
	counter := 1

	val, err := requestSonic(context.Background(), client, token, http.MethodGet, dailyTransfer, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot send request: %w", err)
	}
	var daily DailyTransfer
	err = json.Unmarshal(val, &daily)
	if err != nil {
		return nil, fmt.Errorf("cannot parse response: %w", err)
	}
	if daily.Data.TotalTransactions < 10 {
		return nil, fmt.Errorf("daily tx not yet, waitting, tx now %v", daily.Data.TotalTransactions)
	}
	for counter <= maxStage {
		stage.Stage = counter
		data, err := json.Marshal(stage)
		if err != nil {
			return nil, fmt.Errorf("cannot marshal : %w", err)
		}
		byStage, err := requestSonic(context.Background(), client, token, http.MethodPost, claimBox, data)
		if err != nil {
			return nil, fmt.Errorf("cannot claim box : %w", err)
		}
		var boxClaim BoxClaimed
		err = json.Unmarshal(byStage, &boxClaim)
		if err != nil {
			return nil, fmt.Errorf("cannot unmarshal response: %w", err)
		}
		fmt.Printf("claim success stage %v ->", boxClaim.Data.Stage)
		time.Sleep(4 * time.Second)
		counter++

	}

	return nil, nil

}
func checkBalance(ctx context.Context, solClient *rpc.Client) error {
	val, err := solClient.GetMinimumBalanceForRentExemption(ctx, 0, "")
	if err != nil {
		return fmt.Errorf("cannot getMinimum ->%v", err)
	}
	res := val / solana.LAMPORTS_PER_SOL
	fmt.Println("val->", val, res)
	return fmt.Errorf("minimum balance required for rent exemption: ${%v} SOL", res)
}

// difference user difference .profile, need interact with filesystem,
func getTokenFromProfile(key string) (interface{}, error) {
	basePath := os.Getenv("HOME")
	profile := basePath + "/.zshenv"

	fil, err := os.OpenFile(profile, os.O_RDONLY, os.ModePerm)
	if err != nil {
		return nil, fmt.Errorf("cannot open profile: %v", err)
	}
	defer fil.Close()
	var str string
	bio := bufio.NewScanner(fil)
	keyExport := fmt.Sprintf("%s=", key)
	for bio.Scan() {
		txt := bio.Text()
		if strings.Contains(txt, keyExport) {
			str = txt[len(keyExport):]

		}
	}
	if err = bio.Err(); err != nil {
		return nil, fmt.Errorf("cannot read file -> %v", err)
	}
	return str, nil
}

func sonicTokenResponse(client *http.Client, payer solana.PrivateKey) (string, error) {
	query := url.Values{
		"wallet": {payer.PublicKey().String()},
	}
	base := sonicChallegen + query.Encode()
	resp, err := requestSonic(context.Background(), client, "", http.MethodGet, base, nil)
	if err != nil {
		return "", fmt.Errorf("cannot get sign from Sonic -> %v", err)
	}
	var response Response
	err = json.Unmarshal(resp, &response)
	if err != nil {
		return "", fmt.Errorf("%v", err)
	}
	signature, err := payer.Sign([]byte(response.Data))
	if err != nil {
		return "", fmt.Errorf("cannot sign payload: %v", err)
	}
	signatureBase64 := base64.StdEncoding.EncodeToString(signature[:])
	encodeAdd := base64.StdEncoding.EncodeToString(payer.PublicKey().Bytes())
	payload := Payload{
		PublicAddr:   payer.PublicKey().String(),
		PublicEncode: encodeAdd,
		Signature:    signatureBase64,
	}
	payloadData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("cannot marshal struct ->%v", err)
	}
	val, err := requestSonic(context.Background(), client, "", http.MethodPost, sonicAuthor, payloadData)
	if err != nil {
		return "", fmt.Errorf("cannot get sonic token -> %v", err)
	}
	sonic := SonicToken{}
	if err = json.Unmarshal(val, &sonic); err != nil {
		return "", fmt.Errorf("cannot unmarshal -> %v", err)
	}
	return sonic.Data.Token, nil
}

func aboutProfile(ctx context.Context, client *http.Client, token, url string) (*SonicProfile, error) {
	response, err := requestSonic(ctx, client, token, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot make request ->%v", err)
	}
	fmt.Println("Body ->", string(response))
	var profile SonicProfile
	if err := json.Unmarshal(response, &profile); err != nil {
		return nil, fmt.Errorf("cannot unmarshal response body ->%v", err)
	}
	return &profile, nil
}

// Todo : will interact with filesystem
func loadSonicPlayers() ([]Player, error) {
	var (
		playerArray []Player
	)
	path, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("cannot get current %v", err)
	}
	if _, err := os.Stat(filepath.Join(path, baseFile)); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("file not exist -> %v", err)
	}
	playerDoc, err := os.ReadFile(filepath.Join(path, baseFile))
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(playerDoc, &playerArray); err != nil {
		return nil, err
	}
	return playerArray, nil
}
func Roll() uint64 {
	luckedArr := []uint64{
		3333,
		4444,
		5555,
		7777,
		9999,
		2222,
		1111,
	}
	rand.NewSource(time.Now().UnixNano())
	return luckedArr[rand.Intn(len(luckedArr))]

}
func SonicTransfer(cluster *rpc.Client, payer solana.PrivateKey, toAddr solana.PublicKey) error {
	//amount := uint64(int64(0.004))

	recent, err := cluster.GetRecentBlockhash(context.Background(), rpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("cannot get recent blockHash -> %v", err)
	}
	rand.NewSource(time.Now().UnixNano())

	tx, err := solana.NewTransaction(
		[]solana.Instruction{
			system.NewTransferInstruction(
				solana.LAMPORTS_PER_SOL/Roll(),
				payer.PublicKey(), toAddr).Build(),
		},
		recent.Value.Blockhash,
		solana.TransactionPayer(payer.PublicKey()),
	)
	if err != nil {
		return fmt.Errorf("new transaction  -> %v", err)
	}
	_, err = tx.Sign(
		func(key solana.PublicKey) *solana.PrivateKey {
			if payer.PublicKey().Equals(key) {
				return &payer
			}
			return nil
		})

	time.Sleep(5 * time.Second)

	_, err = cluster.SendTransaction(context.Background(), tx)
	if err != nil {
		return fmt.Errorf("send transaction  -> %v", err)
	}
	return nil
}

type Response struct {
	Code    int    `json:"code"`
	Data    string `json:"data"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func generatorSolAccounts() (interface{}, error) {

	var (
		payer    Player
		payerArr []Player
	)
	path, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("cannot get current %v", err)
	}
	fil, err := os.OpenFile(filepath.Join(path, baseFile), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("cannot openfile, check file %v", err)
	}

	for i := 0; i < 10; i++ {
		val, err := solana.NewRandomPrivateKey()
		if err != nil {
			return nil, fmt.Errorf("cannot generate random  key -> %v", err)
		}
		payer.Idx = fmt.Sprintf("Index: %v", i)
		payer.Pub = val.PublicKey().String()
		payer.Prik = val.String()
		payerArr = append(payerArr, payer)
	}
	data, err := json.MarshalIndent(payerArr, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("cannot marshal payer -> %v", err)
	}
	defer fil.Close()
	return fil.WriteString(string(data))
}
