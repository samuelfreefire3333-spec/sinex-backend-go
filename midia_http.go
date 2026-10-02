package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// ==========================================================================
// POST /midia/assinar — autoriza um envio de arquivo ao Cloudinary
// ==========================================================================
// Quem pede precisa estar logado (ID token do Firebase). O servidor escolhe o
// destino do arquivo e devolve a assinatura; o navegador envia o arquivo
// direto ao Cloudinary com ela. A assinatura vale para UM destino, não deixa
// sobrescrever arquivo existente e expira (o Cloudinary recusa timestamp com
// mais de uma hora).
//
//   destino "midia"  + chatId: só participante da conversa. O arquivo sobe
//                    como "raw" (bytes como estão), em midia/{chatId}/{uid}/…
//   destino "avatar": foto de perfil da própria conta, em avatares/{uid}/…,
//                    aceita só JPEG.

// Envios assinados por conta: rajada de 20, 30 por minuto.
var assinaturasDeMidia = NovoLimitadorPorChave(30, 20)

type pedidoDeAssinatura struct {
	Destino string `json:"destino"`
	ChatID  string `json:"chatId"`
}

type respostaDeAssinatura struct {
	Nuvem      string            `json:"nuvem"`
	ChaveAPI   string            `json:"chaveApi"`
	Recurso    string            `json:"recurso"` // "raw" | "image"
	Parametros map[string]string `json:"parametros"`
	Assinatura string            `json:"assinatura"`
}

func (s *servidor) serveAssinarMidia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		responderErro(w, http.StatusMethodNotAllowed, "metodo_nao_permitido")
		return
	}
	tok, ok := s.autenticar(w, r, false)
	if !ok {
		return
	}
	if !s.cfg.CloudinaryConfigurado() {
		responderErro(w, http.StatusServiceUnavailable, "midia_indisponivel")
		return
	}
	if !reUIDNoCaminho.MatchString(tok.UID) {
		responderErro(w, http.StatusForbidden, "sem_permissao")
		return
	}
	if !assinaturasDeMidia.Permitir(tok.UID) {
		w.Header().Set("Retry-After", "60")
		responderErro(w, http.StatusTooManyRequests, "muitas_requisicoes")
		return
	}

	var pedido pedidoDeAssinatura
	if status, codigo := lerJSON(w, r, 1024, &pedido); status != 0 {
		responderErro(w, status, codigo)
		return
	}

	id, err := uuidV4()
	if err != nil {
		slog.Error("falha ao gerar id de midia", "erro", err)
		responderErro(w, http.StatusServiceUnavailable, "indisponivel")
		return
	}

	parametros := map[string]string{
		"overwrite": "false",
		"timestamp": strconv.FormatInt(time.Now().Unix(), 10),
	}
	recurso := "raw"

	switch pedido.Destino {
	case "midia":
		if s.svc == nil || s.svc.autorizador == nil {
			responderErro(w, http.StatusServiceUnavailable, "indisponivel")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if _, err := s.svc.autorizador.Autorizar(ctx, tok.UID, pedido.ChatID); err != nil {
			switch {
			case errors.Is(err, ErrConversaInvalida):
				responderErro(w, http.StatusBadRequest, "conversa_invalida")
			case errors.Is(err, ErrSemConversa), errors.Is(err, ErrNaoParticipante):
				s.seguranca.Registrar(EventoDeSeguranca{Tipo: "midia_sem_permissao", UID: tok.UID, IP: ipDoCliente(r, s.cfg.ConfiarProxy)})
				responderErro(w, http.StatusForbidden, "sem_permissao")
			case errors.Is(err, ErrMuitasConsultas):
				w.Header().Set("Retry-After", "60")
				responderErro(w, http.StatusTooManyRequests, "muitas_requisicoes")
			default:
				slog.Error("falha ao conferir conversa para midia", "erro", err)
				responderErro(w, http.StatusServiceUnavailable, "indisponivel")
			}
			return
		}
		parametros["public_id"] = caminhoDeMidia(pedido.ChatID, tok.UID, id)
	case "avatar":
		if pedido.ChatID != "" {
			responderErro(w, http.StatusBadRequest, "pedido_invalido")
			return
		}
		recurso = "image"
		parametros["public_id"] = caminhoDeAvatar(tok.UID, id)
		parametros["allowed_formats"] = "jpg"
	default:
		responderErro(w, http.StatusBadRequest, "pedido_invalido")
		return
	}

	// Resposta nunca vai para cache: a assinatura é de uso imediato.
	w.Header().Set("Cache-Control", "no-store")
	responderJSON(w, http.StatusOK, respostaDeAssinatura{
		Nuvem:      s.cfg.CloudinaryNuvem,
		ChaveAPI:   s.cfg.CloudinaryChave,
		Recurso:    recurso,
		Parametros: parametros,
		Assinatura: assinarCloudinary(parametros, s.cfg.CloudinarySegredo),
	})
}
