package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"pi/util"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stellar/go/keypair"
	"github.com/stellar/go/protocols/horizon"
)

type WithdrawRequest struct {
	SeedPhrase        string `json:"seed_phrase"`
	LockedBalanceID   string `json:"locked_balance_id"`
	WithdrawalAddress string `json:"withdrawal_address"`
	Amount            string `json:"amount"`
}

type WithdrawResponse struct {
	Time             string  `json:"time"`
	AttemptNumber    int     `json:"attempt_number"`
	RecipientAddress string  `json:"recipient_address"`
	SenderAddress    string  `json:"sender_address"`
	Amount           float64 `json:"amount"`
	Success          bool    `json:"success"`
	Message          string  `json:"message"`
	Action           string  `json:"action"`
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var writeMu sync.Mutex

func (s *Server) Withdraw(ctx *gin.Context) {
	conn, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		ctx.JSON(500, gin.H{"message": "Failed to upgrade to WebSocket"})
		return
	}
	//defer conn.Close()

	var req WithdrawRequest
	_, message, err := conn.ReadMessage()
	if err != nil {
		conn.WriteJSON(gin.H{"message": "Invalid request"})
		return
	}

	err = json.Unmarshal(message, &req)
	if err != nil {
		conn.WriteJSON(gin.H{"message": "Malformed JSON"})
		return
	}

	kp, err := util.GetKeyFromSeed(req.SeedPhrase)
	if err != nil {
		ctx.AbortWithStatusJSON(400, gin.H{
			"message": "invalid seed phrase",
		})
		return
	}

	availableBalance, err := s.wallet.GetAvailableBalance(kp)
	if err != nil {
		res := WithdrawResponse{
			Action:  "withdrawn",
			Message: "error withdrawing available balance " + err.Error(),
			Success: true,
		}
		s.sendResponse(conn, res)
		//return
	}

	err = s.wallet.Transfer(kp, availableBalance, req.WithdrawalAddress)
	fmt.Println(err)
	if err == nil {
		res := WithdrawResponse{
			Action:  "withdrawn",
			Message: "successfully withdrawn available balance",
			Success: true,
		}
		s.sendResponse(conn, res)
	}

	s.scheduleWithdraw(conn, kp, req)
}

func (s *Server) scheduleWithdraw(conn *websocket.Conn, kp *keypair.Full, req WithdrawRequest) {
	balance, err := s.wallet.GetClaimableBalance(req.LockedBalanceID)
	if err != nil {
		s.sendResponse(conn, WithdrawResponse{
			Message: err.Error(),
			Success: false,
		})
		return
	}

	for _, claimant := range balance.Claimants {
		if claimant.Destination == kp.Address() {
			claimableAt, ok := util.ExtractClaimableTime(claimant.Predicate)
			if !ok {
				s.sendErrorResponse(conn, "error finding locked balance unlock date")
				return
			}

			res := WithdrawResponse{
				Action:  "schedule",
				Message: "Scheduled withdrawal of locked balance for " + claimableAt.Format("Jan 2 3:04 PM"),
				Success: true,
			}
			s.sendResponse(conn, res)

			waitDuration := time.Until(claimableAt)
			if waitDuration < 0 {
				waitDuration = 0
			}

			time.AfterFunc(waitDuration, func() {
				s.claimAndTransfer(conn, kp, balance, req)
			})
		}
	}
}

func (s *Server) claimAndTransfer(conn *websocket.Conn, kp *keypair.Full, balance *horizon.ClaimableBalance, req WithdrawRequest) {
	defer conn.Close()
	senderAddress := kp.Address()

	res := WithdrawResponse{
		Action:  "started_withdrawal",
		Message: "Started scheduled withdrawal of locked balance",
		Success: true,
	}
	s.sendResponse(conn, res)

	for i := 0; i < 100; i++ {
		currentAttempt := i + 1
		hash, amount, err := s.wallet.WithdrawClaimableBalance(kp, req.Amount, req.LockedBalanceID, req.WithdrawalAddress)
		if err == nil {
			s.sendResponses(conn, req, currentAttempt, true, amount, senderAddress, hash)
		} else {
			s.sendResponses(conn, req, currentAttempt, false, amount, senderAddress, err.Error())
		}

		time.Sleep(600 * time.Millisecond)
	}
}

func (s *Server) sendResponse(conn *websocket.Conn, res WithdrawResponse) {
	writeMu.Lock()
	defer writeMu.Unlock()
	conn.WriteJSON(res)
}

func (s *Server) sendErrorResponse(conn *websocket.Conn, msg string) {
	writeMu.Lock()
	defer writeMu.Unlock()
	res := WithdrawResponse{
		Success: false,
		Message: msg,
	}

	conn.WriteJSON(res)
}

func (s *Server) sendResponses(conn *websocket.Conn, req WithdrawRequest, attempt int, success bool, amount float64, senderAddress, msg string) {
	res := WithdrawResponse{
		Time:             time.Now().Format("15:04:05"),
		AttemptNumber:    attempt,
		RecipientAddress: req.WithdrawalAddress,
		SenderAddress:    senderAddress,
		Amount:           amount,
		Success:          success,
		Message:          msg,
	}

	writeMu.Lock()
	defer writeMu.Unlock()
	conn.WriteJSON(res)
}
