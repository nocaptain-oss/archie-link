package controlplane

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/aipay"
)

func handleCreateOrder(service *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Plan string `json:"plan"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		order, err := service.CreateOrder(body.Plan)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, order)
	}
}

func handlePayment(service *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			EventID   string `json:"event_id"`
			Status    string `json:"status"`
			Timestamp string `json:"timestamp"`
			Signature string `json:"signature"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		timestamp := time.Now()
		if body.Timestamp != "" {
			parsed, err := time.Parse(time.RFC3339, body.Timestamp)
			if err != nil {
				writeError(w, ErrEventRejected)
				return
			}
			timestamp = parsed
		}
		if err := service.NotifyPayment(r.PathValue("id"), body.EventID, body.Status, timestamp, body.Signature); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func handleChallenge(service *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			InstallID string `json:"install_id"`
			PublicKey string `json:"device_public_key"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		challenge, err := service.IssueChallenge(body.InstallID, body.PublicKey)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"nonce_id":   challenge.ID,
			"challenge":  base64.StdEncoding.EncodeToString(challenge.Value),
			"expires_at": challenge.ExpiresAt.Format(time.RFC3339),
		})
	}
}

func handleActivate(service *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			OrderID   string `json:"order_id"`
			InstallID string `json:"install_id"`
			PublicKey string `json:"device_public_key"`
			NonceID   string `json:"nonce_id"`
			Signature string `json:"signature"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		license, err := service.Activate(body.OrderID, body.InstallID, body.PublicKey, body.NonceID, body.Signature)
		writeLease(w, license, err)
	}
}

func handleRenew(service *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			OrderID   string `json:"order_id"`
			InstallID string `json:"install_id"`
			PublicKey string `json:"device_public_key"`
			NonceID   string `json:"nonce_id"`
			Signature string `json:"signature"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		license, err := service.Renew(body.OrderID, body.InstallID, body.PublicKey, body.NonceID, body.Signature)
		writeLease(w, license, err)
	}
}

func handleRefund(service *Service, payment *PaymentService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var err error
		if payment != nil {
			err = payment.Refund(r.Context(), r.PathValue("id"))
		} else {
			err = service.Refund(r.PathValue("id"))
		}
		if errors.Is(err, ErrRefundPending) {
			writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "pending": true})
			return
		}
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func handleChargeback(service *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := service.Chargeback(r.PathValue("id")); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func handleDevices(service *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		devices, err := service.ListDevices(r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
	}
}

func handleRemoveDevice(service *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := service.RemoveDevice(r.PathValue("id"), r.PathValue("install_id")); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func handleEpoch(service *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		epoch, err := service.CurrentEpoch(r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"revocation_epoch": epoch})
	}
}

func handleActivationIntent(payment *PaymentService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		var request ActivationIntentRequest
		if !decodeBody(w, r, &request) {
			return
		}
		proof := r.Header.Get("Payment-Proof")
		if proof == "" {
			grant, bill, err := payment.Begin(r.Context(), request)
			if err != nil {
				writePaymentError(w, err)
				return
			}
			if bill.Protocol.OutTradeNo == "" {
				if header, encodeErr := encodePaymentValidation(grant); encodeErr == nil {
					w.Header().Set("Payment-Validation", header)
				}
				writeJSON(w, http.StatusOK, activationResource(grant))
				return
			}
			header, err := encodePaymentNeeded(bill)
			if err != nil {
				writePaymentError(w, err)
				return
			}
			w.Header().Set("Payment-Needed", header)
			writeJSON(w, http.StatusPaymentRequired, map[string]any{
				"code": "Payment-Needed", "out_trade_no": bill.Protocol.OutTradeNo,
				"amount": bill.Protocol.Amount, "currency": bill.Protocol.Currency,
				"goods_name": bill.Method.GoodsName, "resource_id": bill.Protocol.ResourceID,
				"pay_before": bill.Protocol.PayBefore,
			})
			return
		}
		grant, err := payment.Complete(r.Context(), request, proof)
		if err != nil {
			writePaymentError(w, err)
			return
		}
		header, err := encodePaymentValidation(grant)
		if err != nil {
			writePaymentError(w, err)
			return
		}
		w.Header().Set("Payment-Validation", header)
		writeJSON(w, http.StatusOK, activationResource(grant))
	}
}

func activationResource(grant ActivationGrant) map[string]any {
	return map[string]any{
		"resource_id": grant.ResourceID,
		"content": map[string]any{
			"order_id":          grant.OrderID,
			"subscription_id":   grant.SubscriptionID,
			"plan":              grant.Plan,
			"amount":            grant.Amount,
			"currency":          grant.Currency,
			"trade_no":          grant.TradeNo,
			"already_fulfilled": grant.AlreadyFulfilled,
		},
	}
}

func writePaymentError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, ErrPaymentIntentInvalid), errors.Is(err, ErrPaymentIntentConflict):
		status = http.StatusBadRequest
	case errors.Is(err, ErrPaymentIntentExpired):
		status = http.StatusGone
	case errors.Is(err, ErrPaymentProofInvalid), errors.Is(err, ErrPaymentMismatch):
		status = http.StatusPaymentRequired
	case errors.Is(err, ErrPaymentTradeReused):
		status = http.StatusConflict
	case errors.Is(err, ErrPaymentPendingConfirm):
		status = http.StatusConflict
	case errors.Is(err, ErrFulfillmentFailed), errors.Is(err, aipay.ErrGateway):
		status = http.StatusBadGateway
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status = http.StatusGatewayTimeout
	}
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func decodeBody(w http.ResponseWriter, r *http.Request, target any) bool {
	if r.Body == nil {
		return true
	}
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return false
	}
	return true
}

func writeLease(w http.ResponseWriter, license []byte, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"license": json.RawMessage(license)})
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, httpStatusFor(err), map[string]any{"error": err.Error()})
}

func httpStatusFor(err error) int {
	switch {
	case errors.Is(err, ErrOrderNotFound), errors.Is(err, ErrSubscriptionNotFound),
		errors.Is(err, ErrDeviceUnknown):
		return http.StatusNotFound
	case errors.Is(err, ErrPlanUnknown):
		return http.StatusBadRequest
	case errors.Is(err, ErrAuthNotConfigured):
		return http.StatusInternalServerError
	case errors.Is(err, ErrChallengeInvalid), errors.Is(err, ErrSignatureInvalid),
		errors.Is(err, ErrEventRejected), errors.Is(err, ErrEventDuplicate):
		return http.StatusForbidden
	case errors.Is(err, ErrOrderNotPaid), errors.Is(err, ErrSubscriptionInactive),
		errors.Is(err, ErrDeviceQuota):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
