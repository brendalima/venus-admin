# Vênus Oasis — Admin

Backend e painel de cadastro de produtos do [venus-site](../venus-site).

**O site público continua estático.** Este serviço não atende visitantes: ele hospeda o admin e expõe os produtos para o *build* do site. Se este backend estiver desligado, caído ou em deploy, o site segue no ar normalmente.

```
admin salva → clica "Publicar"
   → webhook do Cloudflare Pages
   → build do site busca /api/products
   → site estático republicado (~1 min)
```

## Rodando

Requer Go 1.27 (fixado em `.tool-versions`, via asdf).

```bash
cp .env.example .env
make run
```

Sobe em `http://localhost:8080`. Sem banco configurado o servidor inicia mesmo assim e `/readyz` avisa — útil para mexer em tela sem depender do Postgres.

Em desenvolvimento o `.env.local` (gerado pelo `neon init`) é carregado automaticamente. Variável já exportada no ambiente continua tendo precedência.

| Comando | O que faz |
|---|---|
| `make run` | Servidor em desenvolvimento |
| `make test` | Testes com detector de corrida |
| `make check` | Tudo que o CI roda: gofmt, vet, testes |
| `make cover` | Relatório de cobertura em HTML |
| `make build` | Binário em `bin/server` |

## Endpoints

| Rota | Para quê |
|---|---|
| `GET /healthz` | Vivacidade — **não** consulta o banco |
| `GET /readyz` | Prontidão — consulta o banco |
| `GET /admin` | Painel (em construção) |

A separação entre os dois é proposital. O Fly usa `/healthz` para decidir se a máquina está saudável, e o Neon no plano gratuito dorme quando ocioso: se a vivacidade dependesse do banco, a máquina seria marcada como doente a cada cochilo do Postgres.

## Estrutura

```
cmd/server/      binário do servidor
cmd/adduser/     criação de usuário do admin (M1)
internal/
  config/        configuração vinda do ambiente
  web/           rotas, middleware, handlers
  store/         consultas geradas pelo sqlc (M1)
  images/        libvips + envio para o R2 (M2)
  publish/       webhook de publicação (M3)
migrations/      goose
```

## Configuração

Tudo por variável de ambiente — é o que o Fly espera (`fly secrets`) e evita credencial no repositório.

| Variável | Padrão | Obrigatória |
|---|---|---|
| `APP_ENV` | `dev` | não (`dev` ou `prod`) |
| `PORT` | `8080` | não |
| `DATABASE_URL_UNPOOLED` | — | sim (preferida) |
| `DATABASE_URL` | — | usada se a de cima faltar |

### Por que a conexão direta, e não a com pooler

O Neon expõe duas: `DATABASE_URL` passa por PgBouncer e `DATABASE_URL_UNPOOLED` vai direto. O app usa a **direta**, porque o pgx já mantém o próprio pool — empilhá-lo sobre o pooling por transação do PgBouncer quebra o cache de prepared statements, e com um processo só e pouca concorrência não há nada a ganhar em troca.

O pool usa `MinConns = 0` de propósito: o compute do Neon suspende quando fica ocioso, e segurar conexão aberta o impediria de dormir, queimando horas do plano gratuito com o admin parado.

## Deploy

Fly.io, região `gru` (São Paulo), máquina de 256 MB que **desliga quando ninguém usa** e religa na próxima requisição. O binário Go sobe em dezenas de milissegundos, então `auto_stop_machines = "stop"` basta — não precisa do `suspend`, que é mais caro.

```bash
fly launch --no-deploy   # primeira vez
fly secrets set DATABASE_URL="..."
fly deploy
```

## Banco

Postgres 18 no **Neon**, projeto `venus`, região `aws-sa-east-1` (São Paulo) — mesma região do app no Fly.

As migrações ficam em `internal/store/migrations/`, embutidas no binário com `go:embed`, e são aplicadas na inicialização. Com uma única máquina isso é seguro e dispensa um passo separado de deploy; se um dia houver mais de uma instância, vira um `release_command` no `fly.toml` para duas não migrarem ao mesmo tempo.

```bash
make test                       # testes de integração pulam sem DATABASE_URL
go test ./internal/store/ -v    # com .env.local presente, rodam de verdade
```

## Estado atual

**M0 concluído:** servidor, configuração, middleware, health checks, encerramento gracioso, Docker, fly.toml, CI.

**M1 parcial:** banco Neon conectado, esquema do catálogo migrado (`products`, `product_images`, `site_state`), testes de integração.

A seguir — M1: sqlc, autenticação e as telas de CRUD.
