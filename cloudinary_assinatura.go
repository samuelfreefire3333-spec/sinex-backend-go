package main

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ==========================================================================
// Assinatura de envio para o Cloudinary (só biblioteca padrão)
// ==========================================================================
// O plano gratuito do Firebase não tem mais Storage, então fotos e áudios
// ficam no Cloudinary. O navegador envia o arquivo direto para lá, mas só
// com uma assinatura feita aqui: o segredo da conta nunca sai do servidor, e
// quem decide o destino do arquivo (public_id) é este código, não o cliente.

// assinarCloudinary monta a assinatura do Upload API: os parâmetros em ordem
// alfabética, no formato chave=valor unidos por "&", seguidos do segredo,
// passados por SHA-1 (o algoritmo padrão das contas do Cloudinary).
func assinarCloudinary(parametros map[string]string, segredo string) string {
	chaves := make([]string, 0, len(parametros))
	for k := range parametros {
		chaves = append(chaves, k)
	}
	sort.Strings(chaves)
	partes := make([]string, 0, len(chaves))
	for _, k := range chaves {
		partes = append(partes, k+"="+parametros[k])
	}
	soma := sha1.Sum([]byte(strings.Join(partes, "&") + segredo))
	return hex.EncodeToString(soma[:])
}

// uuidV4 gera um identificador aleatório no formato que o front e as regras
// do Firestore esperam no fim do caminho da mídia.
func uuidV4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// O uid entra no caminho do arquivo: só o formato de uid do Firebase Auth.
var reUIDNoCaminho = regexp.MustCompile(`^[A-Za-z0-9]{1,128}$`)

// caminhoDeMidia é o mesmo formato que o midia-validacao.js confere:
// midia/{chatId}/{uid}/{uuid}.
func caminhoDeMidia(chatID, uid, id string) string {
	return "midia/" + chatID + "/" + uid + "/" + id
}

// caminhoDeAvatar: avatares/{uid}/{uuid}. As regras do Firestore só aceitam
// no perfil uma foto cujo caminho tenha o uid de quem grava.
func caminhoDeAvatar(uid, id string) string {
	return "avatares/" + uid + "/" + id
}
