package main

import (
	"errors"
	"fmt"
	"net/http"

	echologrus "github.com/davrux/echo-logrus/v4"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"gorm.io/gorm"

	"github.com/getAlby/nostr-wallet-connect/alby"
	"github.com/getAlby/nostr-wallet-connect/events"
	models "github.com/getAlby/nostr-wallet-connect/models/http"

	"github.com/getAlby/nostr-wallet-connect/frontend"
	"github.com/getAlby/nostr-wallet-connect/models/api"
)

type HttpService struct {
	svc         *Service
	api         *API
	albyHttpSvc *alby.AlbyHttpService
}

const (
	sessionCookieName    = "session"
	sessionCookieAuthKey = "authenticated"
)

func NewHttpService(svc *Service) *HttpService {
	return &HttpService{
		svc:         svc,
		api:         NewAPI(svc),
		albyHttpSvc: alby.NewAlbyHttpService(svc.AlbyOAuthSvc, svc.Logger),
	}
}

func (httpSvc *HttpService) validateUserMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if !httpSvc.isUnlocked(c) {
			return c.NoContent(http.StatusUnauthorized)
		}
		return next(c)
	}
}

func (httpSvc *HttpService) RegisterSharedRoutes(e *echo.Echo) {
	e.HideBanner = true
	e.Use(echologrus.Middleware())
	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())
	e.Use(middleware.CSRFWithConfig(middleware.CSRFConfig{
		TokenLookup: "header:X-CSRF-Token",
	}))
	e.Use(session.Middleware(sessions.NewCookieStore([]byte(httpSvc.svc.cfg.CookieSecret))))

	authMiddleware := httpSvc.validateUserMiddleware
	e.GET("/api/apps", httpSvc.appsListHandler, authMiddleware)
	e.GET("/api/apps/:pubkey", httpSvc.appsShowHandler, authMiddleware)
	e.PATCH("/api/apps/:pubkey", httpSvc.appsUpdateHandler, authMiddleware)
	e.DELETE("/api/apps/:pubkey", httpSvc.appsDeleteHandler, authMiddleware)
	e.POST("/api/apps", httpSvc.appsCreateHandler, authMiddleware)
	e.GET("/api/encrypted-mnemonic", httpSvc.encryptedMnemonicHandler, authMiddleware)
	e.PATCH("/api/backup-reminder", httpSvc.backupReminderHandler, authMiddleware)

	e.GET("/api/csrf", httpSvc.csrfHandler)
	e.GET("/api/info", httpSvc.infoHandler)
	e.POST("/api/logout", httpSvc.logoutHandler)
	e.POST("/api/setup", httpSvc.setupHandler)

	// allow one unlock request per second
	unlockRateLimiter := middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(1))
	e.POST("/api/start", httpSvc.startHandler, unlockRateLimiter)
	e.POST("/api/unlock", httpSvc.unlockHandler, unlockRateLimiter)
	e.PATCH("/api/unlock-password", httpSvc.changeUnlockPasswordHandler, unlockRateLimiter)

	// TODO: below could be supported by NIP-47
	e.GET("/api/channels", httpSvc.channelsListHandler, authMiddleware)
	e.POST("/api/channels", httpSvc.openChannelHandler, authMiddleware)
	// TODO: review naming
	e.POST("/api/instant-channel-invoices", httpSvc.newInstantChannelInvoiceHandler, authMiddleware)
	// TODO: should this be DELETE /api/channels:id?
	e.POST("/api/channels/close", httpSvc.closeChannelHandler, authMiddleware)
	e.GET("/api/node/connection-info", httpSvc.nodeConnectionInfoHandler, authMiddleware)
	e.GET("/api/peers", httpSvc.listPeers, authMiddleware)
	e.POST("/api/peers", httpSvc.connectPeerHandler, authMiddleware)
	e.POST("/api/wallet/new-address", httpSvc.newOnchainAddressHandler, authMiddleware)
	e.POST("/api/wallet/redeem-onchain-funds", httpSvc.redeemOnchainFundsHandler, authMiddleware)
	e.GET("/api/balances", httpSvc.balancesHandler, authMiddleware)
	e.POST("/api/reset-router", httpSvc.resetRouterHandler, authMiddleware)
	e.POST("/api/stop", httpSvc.stopHandler, authMiddleware)

	httpSvc.albyHttpSvc.RegisterSharedRoutes(e, authMiddleware)

	e.GET("/api/mempool/lightning/nodes/:pubkey", httpSvc.mempoolLightningNodeHandler, authMiddleware)

	e.POST("/api/send-payment-probes", httpSvc.sendPaymentProbesHandler, authMiddleware)
	e.POST("/api/send-spontaneous-payment-probes", httpSvc.sendSpontaneousPaymentProbesHandler, authMiddleware)
	e.GET("/api/log/:type", httpSvc.getLogOutputHandler, authMiddleware)

	frontend.RegisterHandlers(e)
}

func (httpSvc *HttpService) csrfHandler(c echo.Context) error {
	csrf, _ := c.Get(middleware.DefaultCSRFConfig.ContextKey).(string)
	if csrf == "" {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: "CSRF token not available",
		})
	}
	return c.JSON(http.StatusOK, csrf)
}

func (httpSvc *HttpService) infoHandler(c echo.Context) error {
	responseBody, err := httpSvc.api.GetInfo(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: err.Error(),
		})
	}
	responseBody.Unlocked = httpSvc.isUnlocked(c)
	return c.JSON(http.StatusOK, responseBody)
}

func (httpSvc *HttpService) encryptedMnemonicHandler(c echo.Context) error {
	responseBody := httpSvc.api.GetEncryptedMnemonic()
	return c.JSON(http.StatusOK, responseBody)
}

func (httpSvc *HttpService) backupReminderHandler(c echo.Context) error {
	var backupReminderRequest api.BackupReminderRequest
	if err := c.Bind(&backupReminderRequest); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Message: fmt.Sprintf("Bad request: %s", err.Error()),
		})
	}

	err := httpSvc.api.SetNextBackupReminder(&backupReminderRequest)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: fmt.Sprintf("Failed to store backup reminder: %s", err.Error()),
		})
	}

	return c.NoContent(http.StatusNoContent)
}

func (httpSvc *HttpService) startHandler(c echo.Context) error {
	var startRequest api.StartRequest
	if err := c.Bind(&startRequest); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Message: fmt.Sprintf("Bad request: %s", err.Error()),
		})
	}

	err := httpSvc.api.Start(&startRequest)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: fmt.Sprintf("Failed to start node: %s", err.Error()),
		})
	}

	err = httpSvc.saveSessionCookie(c)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: fmt.Sprintf("Failed to save session: %s", err.Error()),
		})
	}

	return c.NoContent(http.StatusNoContent)
}

func (httpSvc *HttpService) unlockHandler(c echo.Context) error {
	var unlockRequest api.UnlockRequest
	if err := c.Bind(&unlockRequest); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Message: fmt.Sprintf("Bad request: %s", err.Error()),
		})
	}

	if !httpSvc.svc.cfg.CheckUnlockPassword(unlockRequest.UnlockPassword) {
		return c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Message: "Invalid password",
		})
	}

	err := httpSvc.saveSessionCookie(c)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: fmt.Sprintf("Failed to save session: %s", err.Error()),
		})
	}

	httpSvc.svc.EventLogger.Log(&events.Event{
		Event: "nwc_unlocked",
	})

	return c.NoContent(http.StatusNoContent)
}

func (httpSvc *HttpService) changeUnlockPasswordHandler(c echo.Context) error {
	var changeUnlockPasswordRequest api.ChangeUnlockPasswordRequest
	if err := c.Bind(&changeUnlockPasswordRequest); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Message: fmt.Sprintf("Bad request: %s", err.Error()),
		})
	}

	err := httpSvc.api.ChangeUnlockPassword(&changeUnlockPasswordRequest)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: fmt.Sprintf("Failed to change unlock password: %s", err.Error()),
		})
	}

	return c.NoContent(http.StatusNoContent)
}

func (httpSvc *HttpService) isUnlocked(c echo.Context) bool {
	sess, _ := session.Get(sessionCookieName, c)
	return sess.Values[sessionCookieAuthKey] == true
}

func (httpSvc *HttpService) saveSessionCookie(c echo.Context) error {
	sess, _ := session.Get("session", c)
	sess.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7,
		HttpOnly: true,
	}
	sess.Values[sessionCookieAuthKey] = true
	err := sess.Save(c.Request(), c.Response())
	if err != nil {
		httpSvc.svc.Logger.WithError(err).Error("Failed to save session")
	}
	return err
}

func (httpSvc *HttpService) logoutHandler(c echo.Context) error {
	sess, err := session.Get(sessionCookieName, c)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: "Failed to get session",
		})
	}
	sess.Options.MaxAge = -1
	if err := sess.Save(c.Request(), c.Response()); err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: "Failed to save session",
		})
	}
	return c.NoContent(http.StatusNoContent)
}

func (httpSvc *HttpService) channelsListHandler(c echo.Context) error {
	ctx := c.Request().Context()

	channels, err := httpSvc.api.ListChannels(ctx)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: err.Error(),
		})
	}

	return c.JSON(http.StatusOK, channels)
}

func (httpSvc *HttpService) resetRouterHandler(c echo.Context) error {
	ctx := c.Request().Context()

	err := httpSvc.api.ResetRouter(ctx)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: err.Error(),
		})
	}

	return c.NoContent(http.StatusNoContent)
}

func (httpSvc *HttpService) stopHandler(c echo.Context) error {

	err := httpSvc.api.Stop()

	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: err.Error(),
		})
	}

	return c.NoContent(http.StatusNoContent)
}

func (httpSvc *HttpService) nodeConnectionInfoHandler(c echo.Context) error {
	ctx := c.Request().Context()

	info, err := httpSvc.api.GetNodeConnectionInfo(ctx)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: err.Error(),
		})
	}

	return c.JSON(http.StatusOK, info)
}

func (httpSvc *HttpService) balancesHandler(c echo.Context) error {
	ctx := c.Request().Context()

	balances, err := httpSvc.api.GetBalances(ctx)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: err.Error(),
		})
	}

	return c.JSON(http.StatusOK, balances)
}

func (httpSvc *HttpService) mempoolLightningNodeHandler(c echo.Context) error {
	pubkey := c.Param("pubkey")
	if pubkey == "" {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Message: "Invalid pubkey parameter",
		})
	}

	response, err := httpSvc.api.GetMempoolLightningNode(pubkey)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: fmt.Sprintf("Failed to request mempool API: %s", err.Error()),
		})
	}

	return c.JSON(http.StatusOK, response)
}

func (httpSvc *HttpService) listPeers(c echo.Context) error {
	peers, err := httpSvc.svc.lnClient.ListPeers(httpSvc.svc.ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: fmt.Sprintf("Failed to list peers: %s", err.Error()),
		})
	}

	return c.JSON(http.StatusOK, peers)
}

func (httpSvc *HttpService) connectPeerHandler(c echo.Context) error {
	ctx := c.Request().Context()

	var connectPeerRequest api.ConnectPeerRequest
	if err := c.Bind(&connectPeerRequest); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Message: fmt.Sprintf("Bad request: %s", err.Error()),
		})
	}

	err := httpSvc.api.ConnectPeer(ctx, &connectPeerRequest)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: fmt.Sprintf("Failed to connect peer: %s", err.Error()),
		})
	}

	return c.NoContent(http.StatusNoContent)
}

func (httpSvc *HttpService) openChannelHandler(c echo.Context) error {
	ctx := c.Request().Context()

	var openChannelRequest api.OpenChannelRequest
	if err := c.Bind(&openChannelRequest); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Message: fmt.Sprintf("Bad request: %s", err.Error()),
		})
	}

	openChannelResponse, err := httpSvc.api.OpenChannel(ctx, &openChannelRequest)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: fmt.Sprintf("Failed to open channel: %s", err.Error()),
		})
	}

	return c.JSON(http.StatusOK, openChannelResponse)
}

func (httpSvc *HttpService) closeChannelHandler(c echo.Context) error {
	ctx := c.Request().Context()

	var closeChannelRequest api.CloseChannelRequest
	if err := c.Bind(&closeChannelRequest); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Message: fmt.Sprintf("Bad request: %s", err.Error()),
		})
	}

	closeChannelResponse, err := httpSvc.api.CloseChannel(ctx, &closeChannelRequest)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: fmt.Sprintf("Failed to close channel: %s", err.Error()),
		})
	}

	return c.JSON(http.StatusOK, closeChannelResponse)
}

func (httpSvc *HttpService) newInstantChannelInvoiceHandler(c echo.Context) error {
	ctx := c.Request().Context()

	var newWrappedInvoiceRequest api.NewInstantChannelInvoiceRequest
	if err := c.Bind(&newWrappedInvoiceRequest); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Message: fmt.Sprintf("Bad request: %s", err.Error()),
		})
	}

	newWrappedInvoiceResponse, err := httpSvc.api.NewInstantChannelInvoice(ctx, &newWrappedInvoiceRequest)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: fmt.Sprintf("Failed to request wrapped invoice: %s", err.Error()),
		})
	}

	return c.JSON(http.StatusOK, newWrappedInvoiceResponse)
}

func (httpSvc *HttpService) newOnchainAddressHandler(c echo.Context) error {
	ctx := c.Request().Context()

	newAddressResponse, err := httpSvc.api.GetNewOnchainAddress(ctx)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: fmt.Sprintf("Failed to request new onchain address: %s", err.Error()),
		})
	}

	return c.JSON(http.StatusOK, newAddressResponse)
}

func (httpSvc *HttpService) redeemOnchainFundsHandler(c echo.Context) error {
	ctx := c.Request().Context()

	var redeemOnchainFundsRequest api.RedeemOnchainFundsRequest
	if err := c.Bind(&redeemOnchainFundsRequest); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Message: fmt.Sprintf("Bad request: %s", err.Error()),
		})
	}

	redeemOnchainFundsResponse, err := httpSvc.api.RedeemOnchainFunds(ctx, redeemOnchainFundsRequest.ToAddress)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: fmt.Sprintf("Failed to redeem onchain funds: %s", err.Error()),
		})
	}

	return c.JSON(http.StatusOK, redeemOnchainFundsResponse)
}

func (httpSvc *HttpService) appsListHandler(c echo.Context) error {

	apps, err := httpSvc.api.ListApps()

	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: err.Error(),
		})
	}

	return c.JSON(http.StatusOK, apps)
}

func (httpSvc *HttpService) appsShowHandler(c echo.Context) error {
	app := App{}
	findResult := httpSvc.svc.db.Where("nostr_pubkey = ?", c.Param("pubkey")).First(&app)

	if findResult.RowsAffected == 0 {
		return c.JSON(http.StatusNotFound, models.ErrorResponse{
			Message: "App does not exist",
		})
	}

	response := httpSvc.api.GetApp(&app)

	return c.JSON(http.StatusOK, response)
}

func (httpSvc *HttpService) appsUpdateHandler(c echo.Context) error {
	var requestData api.UpdateAppRequest
	if err := c.Bind(&requestData); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Message: fmt.Sprintf("Bad request: %s", err.Error()),
		})
	}

	app := App{}
	findResult := httpSvc.svc.db.Where("nostr_pubkey = ?", c.Param("pubkey")).First(&app)

	if findResult.RowsAffected == 0 {
		return c.JSON(http.StatusNotFound, models.ErrorResponse{
			Message: "App does not exist",
		})
	}

	err := httpSvc.api.UpdateApp(&app, &requestData)

	if err != nil {
		httpSvc.svc.Logger.WithError(err).Error("Failed to update app")
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: fmt.Sprintf("Failed to update app: %v", err),
		})
	}

	return c.NoContent(http.StatusNoContent)
}

func (httpSvc *HttpService) appsDeleteHandler(c echo.Context) error {
	pubkey := c.Param("pubkey")
	if pubkey == "" {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Message: "Invalid pubkey parameter",
		})
	}
	app := App{}
	result := httpSvc.svc.db.Where("nostr_pubkey = ?", pubkey).First(&app)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, models.ErrorResponse{
				Message: "App not found",
			})
		}
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: "Failed to fetch app",
		})
	}

	if err := httpSvc.api.DeleteApp(&app); err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: "Failed to delete app",
		})
	}
	return c.NoContent(http.StatusNoContent)
}

func (httpSvc *HttpService) appsCreateHandler(c echo.Context) error {
	var requestData api.CreateAppRequest
	if err := c.Bind(&requestData); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Message: fmt.Sprintf("Bad request: %s", err.Error()),
		})
	}

	responseBody, err := httpSvc.api.CreateApp(&requestData)

	if err != nil {
		httpSvc.svc.Logger.WithField("requestData", requestData).WithError(err).Error("Failed to save app")
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: fmt.Sprintf("Failed to save app: %v", err),
		})
	}

	return c.JSON(http.StatusOK, responseBody)
}

func (httpSvc *HttpService) setupHandler(c echo.Context) error {
	var setupRequest api.SetupRequest
	if err := c.Bind(&setupRequest); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Message: fmt.Sprintf("Bad request: %s", err.Error()),
		})
	}

	err := httpSvc.api.Setup(c.Request().Context(), &setupRequest)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: fmt.Sprintf("Failed to setup node: %s", err.Error()),
		})
	}

	return c.NoContent(http.StatusNoContent)
}

func (httpSvc *HttpService) sendPaymentProbesHandler(c echo.Context) error {
	var sendPaymentProbesRequest api.SendPaymentProbesRequest
	if err := c.Bind(&sendPaymentProbesRequest); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Message: fmt.Sprintf("Bad request: %s", err.Error()),
		})
	}

	sendPaymentProbesResponse, err := httpSvc.api.SendPaymentProbes(c.Request().Context(), &sendPaymentProbesRequest)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: fmt.Sprintf("Failed to send payment probes: %v", err),
		})
	}

	return c.JSON(http.StatusOK, sendPaymentProbesResponse)
}

func (httpSvc *HttpService) sendSpontaneousPaymentProbesHandler(c echo.Context) error {
	var sendSpontaneousPaymentProbesRequest api.SendSpontaneousPaymentProbesRequest
	if err := c.Bind(&sendSpontaneousPaymentProbesRequest); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Message: fmt.Sprintf("Bad request: %s", err.Error()),
		})
	}

	sendSpontaneousPaymentProbesResponse, err := httpSvc.api.SendSpontaneousPaymentProbes(c.Request().Context(), &sendSpontaneousPaymentProbesRequest)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: fmt.Sprintf("Failed to send spontaneous payment probes: %v", err),
		})
	}

	return c.JSON(http.StatusOK, sendSpontaneousPaymentProbesResponse)
}

func (httpSvc *HttpService) getLogOutputHandler(c echo.Context) error {
	var getLogRequest api.GetLogOutputRequest
	if err := c.Bind(&getLogRequest); err != nil {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Message: fmt.Sprintf("Bad request: %s", err.Error()),
		})
	}

	logType := c.Param("type")
	if logType != api.LogTypeNode && logType != api.LogTypeApp {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Message: fmt.Sprintf("Invalid log type parameter: '%s'", logType),
		})
	}

	getLogResponse, err := httpSvc.api.GetLogOutput(c.Request().Context(), logType, &getLogRequest)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Message: fmt.Sprintf("Failed to get log output: %v", err),
		})
	}

	return c.JSON(http.StatusOK, getLogResponse)
}
