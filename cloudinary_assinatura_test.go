package main

import (
	"regexp"
	"testing"
)

// Vetor da documentação do Cloudinary ("Generating authentication signatures").
func TestAssinarCloudinaryVetorDaDocumentacao(t *testing.T) {
	got := assinarCloudinary(map[string]string{
		"timestamp": "1315060510",
		"public_id": "sample_image",
		"eager":     "w_400,h_300,c_pad|w_260,h_200,c_crop",
	}, "abcd")
	const want = "bfd09f95f331f558cbd1320e67aa8d488770583e"
	if got != want {
		t.Fatalf("assinatura = %s, esperado %s", got, want)
	}
}

func TestAssinaturaMudaComQualquerParametro(t *testing.T) {
	base := map[string]string{"public_id": "midia/a_b/uid1/x", "timestamp": "1", "overwrite": "false"}
	outro := map[string]string{"public_id": "midia/a_b/uid2/x", "timestamp": "1", "overwrite": "false"}
	if assinarCloudinary(base, "s") == assinarCloudinary(outro, "s") {
		t.Fatal("caminhos diferentes deram a mesma assinatura")
	}
	if assinarCloudinary(base, "s") == assinarCloudinary(base, "t") {
		t.Fatal("segredos diferentes deram a mesma assinatura")
	}
}

func TestUUIDV4(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	vistos := map[string]bool{}
	for i := 0; i < 200; i++ {
		id, err := uuidV4()
		if err != nil || !re.MatchString(id) {
			t.Fatalf("uuid inválido: %q (%v)", id, err)
		}
		if vistos[id] {
			t.Fatal("uuid repetido")
		}
		vistos[id] = true
	}
}

func TestCaminhos(t *testing.T) {
	if got := caminhoDeMidia("ana_bruno", "uidAna", "id"); got != "midia/ana_bruno/uidAna/id" {
		t.Fatal(got)
	}
	if got := caminhoDeAvatar("uidAna", "id"); got != "avatares/uidAna/id" {
		t.Fatal(got)
	}
	if reUIDNoCaminho.MatchString("a/b") || reUIDNoCaminho.MatchString("") || !reUIDNoCaminho.MatchString("AbC123") {
		t.Fatal("formato de uid")
	}
}
