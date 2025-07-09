package server

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/stellar/go/keypair"
	"github.com/stellar/go/protocols/horizon"
	"github.com/stellar/go/protocols/horizon/operations"
	"golang.org/x/sync/errgroup"
)

type LoginRequest struct {
	SeedPhrase string `json:"seed_phrase"`
}

type LoginResponse struct {
	AvailableBalance string                     `json:"available_balance"`
	Transactions     []operations.Operation     `json:"transactions"`
	LockedBalnces    []horizon.ClaimableBalance `json:"locked_balances"`
	WalletAddress    string                     `json:"wallet_address"`
	SeedPhrase       string                     `json:"seed_phrase"`
}

func (s *Server) getWalletData(ctx *gin.Context, seedPhrase string, kp *keypair.Full) {
	var (
		availableBalance string
		transactions     []operations.Operation
		lockedBalances   []horizon.ClaimableBalance
	)

	g, _ := errgroup.WithContext(ctx)
	g.Go(func() error {
		balance, err := s.wallet.GetAvailableBalance(kp)
		if err != nil {
			return err
		}
		availableBalance = balance

		return nil
	})

	g.Go(func() error {
		txns, err := s.wallet.GetTransactions(kp, 5)
		if err != nil {
			return err
		}
		transactions = txns
		return nil
	})

	g.Go(func() error {
		lb, err := s.wallet.GetLockedBalances(kp)
		if err != nil {
			return err
		}
		lockedBalances = lb
		return nil
	})

	if err := g.Wait(); err != nil {
		ctx.AbortWithStatusJSON(400, gin.H{
			"message": err.Error(),
		})
		return
	}

	ctx.JSON(200, LoginResponse{
		AvailableBalance: availableBalance,
		Transactions:     transactions,
		LockedBalnces:    lockedBalances,
		WalletAddress:    s.wallet.GetAddress(kp),
		SeedPhrase:       seedPhrase,
	})
}

func (s *Server) Login(ctx *gin.Context) {
	var req LoginRequest

	err := ctx.BindJSON(&req)
	if err != nil {
		ctx.AbortWithStatusJSON(400, gin.H{
			"message": fmt.Sprintf("invalid request body: %v", err),
		})
		return
	}

	kp, err := s.wallet.Login(req.SeedPhrase)
	if err != nil {
		ctx.AbortWithStatusJSON(400, gin.H{
			"message": err.Error(),
		})
		return
	}

	s.getWalletData(ctx, req.SeedPhrase, kp)
}
