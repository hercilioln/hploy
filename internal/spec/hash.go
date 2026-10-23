package spec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Hash resume a configuracao de um servico numa string curta.
//
// E o que torna o `apply` idempotente sem comparar campo por campo com o que
// o Docker devolve -- comparacao que e um inferno, porque o daemon normaliza
// valores (ordem de env, caminho de volume, unidade de memoria) e um diff
// ingenuo acusaria diferenca onde nao ha.
//
// Em vez disso gravamos o hash como label no container. No apply seguinte:
//
//	hash igual     -> nada a fazer
//	hash diferente -> recriar
//
// O hash NAO inclui os valores de env, so os nomes: mudar uma senha nao deve
// obrigar a recriar o container (isso e trabalho do deploy, nao do apply), e
// segredo nao deve participar de identificador que fica visivel em label.
func (sv *Service) Hash(app string, env map[string]string) string {
	h := sha256.New()

	escreve := func(formato string, args ...any) {
		fmt.Fprintf(h, formato+"\x00", args...)
	}

	escreve("app=%s", app)
	escreve("servico=%s", sv.nome)
	escreve("imagem=%s", sv.Image)
	escreve("comando=%s", strings.Join(sv.Command, "\x1f"))
	escreve("porta=%d", sv.Port)
	escreve("memoria=%d", uint64(sv.Resources.Memory))
	escreve("cpus=%s", sv.Resources.CPUs)
	escreve("saude=%s", sv.Health.String())

	// Volumes e env em ordem: a ordem no arquivo nao deve mudar o hash.
	vols := append([]string{}, sv.Volumes...)
	sort.Strings(vols)
	escreve("volumes=%s", strings.Join(vols, "\x1f"))

	nomes := sv.EnvNames()
	escreve("env=%s", strings.Join(nomes, "\x1f"))

	// Os literais SAO configuracao, nao segredo: entram com valor.
	chaves := make([]string, 0, len(sv.Env.Literal))
	for k := range sv.Env.Literal {
		chaves = append(chaves, k)
	}
	sort.Strings(chaves)
	for _, k := range chaves {
		escreve("literal=%s=%s", k, sv.Env.Literal[k])
	}

	return hex.EncodeToString(h.Sum(nil))[:16]
}
