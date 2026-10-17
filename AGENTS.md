# agent.md — Padrões para projetos Go

Este documento orienta agentes que implementam serviços em Go. Siga estas regras ao criar ou
alterar código. A arquitetura é **hexagonal (ports & adapters)**, e um mesmo módulo pode ter
vários binários: API HTTP, worker de mensageria, migração de banco.

Quando houver dúvida, priorize:
**separação de camadas > consistência com o código existente > conveniência**.

Nos exemplos, `Order` e `Item` são entidades fictícias. Troque pelas entidades do seu domínio.

---

## 1. Estrutura de diretórios

```
.
├── cmd/                      # Um diretório por binário (package main). Só composição/wiring.
│   ├── api/                  # Servidor HTTP: main.go (wiring + rotas) e server.go
│   ├── migrate/              # Migração de schema
│   └── worker/               # Consumer de mensagens (Pub/Sub, SQS, Kafka...)
├── adapters/                 # Infra compartilhada: leitura de config (env) e conexão com banco
├── driven/                   # DTOs de persistência (modelo das tabelas, tags do ORM, enums de status)
├── drivers/                  # DTOs de entrada (payloads HTTP, tags json/binding)
├── internal/
│   ├── core/
│   │   ├── domain/           # Entidades e erros de domínio. Sem dependências externas.
│   │   └── services/         # Regras de negócio + contracts.go (interfaces expostas aos handlers)
│   ├── repository/           # Acesso a banco (database/sql) + contracts.go (ports de persistência)
│   ├── storage/              # Adapters de serviços externos (object storage etc.) + contracts.go
│   ├── rest/                 # Handlers HTTP. Tradução HTTP <-> domínio
│   └── subscriber/           # Handlers de mensagens assíncronas
├── Dockerfile
├── docker-compose.yml        # Um serviço por binário em cmd/ + dependências (banco etc.)
├── .env-sample               # Toda variável de ambiente nova entra aqui
└── README.md
```

Regras:
- Todo código de aplicação fica em `internal/`.
- Crie um arquivo por entidade em cada camada (`order.go`, `item.go`...).
- Cada pacote que expõe um port tem um `contracts.go` com **apenas** as interfaces.
- Nomes de pacote no singular e em minúsculas (`repository`, `storage`, `rest`).
- Crie só as camadas de que o projeto precisa. Sem worker, não há `cmd/worker` nem `subscriber`.

## 2. Camadas e direção das dependências

```
cmd ──► rest / subscriber ──► services ──► repository / storage (interfaces)
                                  │
                                  └──► domain  ◄── (todas as camadas)
```

| Camada | Pode importar | Não pode importar |
|---|---|---|
| `domain` | stdlib | qualquer outro pacote do projeto, libs de infra |
| `services` | `domain`, interfaces de `repository`/`storage` | framework HTTP, `database/sql`, SDKs de cloud |
| `repository` | `domain`, `driven`, `database/sql` | `services`, `rest`, framework HTTP |
| `storage` | `domain`, SDK do provedor | `services`, `rest` |
| `rest` | `domain`, `drivers`, interfaces de `services`, framework HTTP | `repository`, `database/sql` |
| `subscriber` | `domain`, interfaces de `services`, SDK de mensageria | `repository`, `storage` diretamente |
| `cmd/*` | tudo — é o composition root | — |

- Services dependem de **interfaces**, nunca de structs concretas de repository ou storage.
- Handlers HTTP e subscribers dependem das **interfaces de `services/contracts.go`**, nunca de
  `*services.Order`. Isso permite usar mocks nos testes.
- Garanta em tempo de compilação que a implementação satisfaz o contrato:
  ```go
  var _ OrderHandler = (*Order)(nil)
  ```
- O domínio trafega entre as camadas. DTOs (`driven`, `drivers`) ficam na borda e são
  convertidos para `domain` (e de volta) dentro do repository ou do handler.

## 3. Modelos: domain x DTOs

- `internal/core/domain`: structs puras, tipos e constantes de domínio. Sem tags de ORM nem de JSON.
  ```go
  type Order struct {
      ID         string
      CustomerID string
      Status     OrderStatus
  }

  type OrderStatus string

  const (
      OrderCreated OrderStatus = "CREATED"
      OrderPaid    OrderStatus = "PAID"
  )
  ```
- `driven`: modelo de persistência com tags do ORM (usadas apenas pela migração).
- `drivers`: payload de entrada com tags `json:"snake_case"` e de validação (`binding:"required"`).
- Respostas HTTP usam structs com tags `json` em `snake_case`, nunca mapas com chaves soltas.

## 4. Construtores

- O padrão `NewX(...) (*X, error)` valida as dependências e retorna erro.
- O padrão `MustNewX(...) *X` chama `NewX` e faz `panic` em caso de erro. Use-o **somente** no
  composition root (`cmd/*`), durante a inicialização.
- Construtores validam as dependências obrigatórias (nil, string vazia, zero):
  ```go
  func NewOrder(repo repository.OrderRepository, storage storage.FileStorage) (*Order, error) {
      if repo == nil {
          return nil, errors.New("missing order repository dependency")
      }
      if storage == nil {
          return nil, errors.New("missing file storage dependency")
      }
      return &Order{repo: repo, storage: storage}, nil
  }

  func MustNewOrder(repo repository.OrderRepository, storage storage.FileStorage) *Order {
      o, err := NewOrder(repo, storage)
      if err != nil {
          panic(fmt.Errorf("couldn't create order service: %w", err))
      }
      return o
  }
  ```
- Quando um construtor recebe muitas dependências, use uma struct de input (`ServiceInput{...}`).
- Campos de dependência são privados (`repo`, `storage`, `db`).
- Injete só o que é usado. Não passe dependências que o tipo não utiliza.

## 5. Composition root (`cmd/*/main.go`)

Ordem de montagem:
1. Carregar a config (`adapters.LoadConfig()`) e tratar o erro.
2. Criar um contexto com cancelamento por sinal (`signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`).
3. Abrir a infra: banco (`adapters.NewDatabase`) e clientes de cloud.
4. Instanciar repositories → adapters externos → services → handlers.
5. Registrar rotas ou assinaturas e iniciar, com shutdown gracioso.

As rotas são registradas no `main.go`, com recursos no plural e ações aninhadas:
```go
router.POST("/orders", rOrder.Create)
router.GET("/orders", rOrder.List)
router.GET("/orders/:id", rOrder.GetByID)
router.POST("/orders/:id/cancel", rOrder.Cancel)
```

Nomeie as variáveis do wiring com o prefixo da camada: `oRepository`, `oService`, `rOrder`.

Nenhuma regra de negócio fica em `cmd/`.

## 6. Configuração

- Toda config vem de variáveis de ambiente, lidas **apenas** em `adapters/config.go` e
  agregadas numa struct `Config`.
- Os valores chegam às camadas explicitamente, via construtor. **Nunca** chame `os.Getenv`
  dentro de `internal/`.
- Converta e valide os tipos na leitura (`strconv.Atoi`, `time.ParseDuration`) e retorne erro se
  algum valor for inválido ou estiver faltando.
- Toda variável nova entra no `.env-sample`. `.env` e arquivos de credenciais nunca vão para o git
  (inclua-os no `.gitignore`).

## 7. Repository (persistência)

- Use `database/sql` com SQL explícito e **sempre** os métodos com contexto:
  `ExecContext`, `QueryContext`, `QueryRowContext`, `BeginTx`.
- Se houver ORM, use-o só para abrir a conexão e migrar o schema. As consultas de runtime são SQL puro.
- **Sempre** use placeholders (`$1`, `$2`...). Nunca monte SQL com `fmt.Sprintf` a partir de
  dados de entrada. Para listas, use `= ANY($1)` / `<> ALL($1)` com `pq.Array(values)`.
- Liste as colunas no `SELECT` (nada de `SELECT *`) e faça o `Scan` na mesma ordem.
- Chame `defer rows.Close()` logo após o `QueryContext` e verifique `rows.Err()` depois do loop.
- Traduza `sql.ErrNoRows` para um erro de domínio:
  ```go
  if errors.Is(err, sql.ErrNoRows) {
      return domain.Order{}, domain.NewNotFoundError("order not found")
  }
  ```
- Faça operações em lote dentro de uma transação, com `defer tx.Rollback()` e `Commit` no fim.
- Prefira upsert (`INSERT ... ON CONFLICT (...) DO UPDATE`) a um select seguido de insert/update.
- Paginação: `LIMIT`/`OFFSET` com `offset = limit * (page - 1)`, e o `limit` vem da config
  injetada. O total vem de uma query `COUNT(*)` separada.

## 8. Services (regra de negócio)

- Recebem `ctx context.Context` como primeiro parâmetro em todos os métodos.
- Concentram as validações de negócio, a geração de IDs (`uuid.New().String()`) e a orquestração
  entre repositories e adapters externos.
- Helpers internos são métodos privados.
- Envolvem os erros das dependências com contexto: `fmt.Errorf("couldn't create order: %w", err)`.
- Retornam valores de domínio, nunca DTOs.

## 9. Handlers HTTP (`internal/rest`)

Fluxo de cada handler:
1. Ler params, query e body (bind para um DTO de `drivers`).
2. Validar a entrada. Se for inválida, responder `400 Bad Request` com `{"error": "..."}`.
3. Converter o DTO para `domain` e chamar o service.
4. Mapear o erro para um status HTTP:
   - erro de validação de domínio → `400`
   - `*domain.NotFoundError` → `404`
   - demais → `500` (sem vazar detalhes internos)
5. Responder com `200`/`201` e JSON.

```go
order, err := h.service.GetByID(ctx, id)
if err != nil {
    var notFound *domain.NotFoundError
    if errors.As(err, &notFound) {
        ctx.JSON(http.StatusNotFound, gin.H{"error": notFound.Error()})
        return
    }
    ctx.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
    return
}
ctx.JSON(http.StatusOK, toOrderResponse(order))
```

Convenções:
- O corpo de erro é sempre `{"error": "<mensagem>"}`.
- Faça `return` logo após cada resposta de erro (early return, sem `else`).
- Paginação via query `page` (default `1`, inteiro > 0), com o total no header `x-total-count`.
- Lista vazia retorna `200` com `[]`, não `404`.
- Handlers não acessam banco nem SDKs de cloud.

## 10. Workers / Subscribers

- O binário em `cmd/worker` cria o cliente, configura a assinatura e delega cada mensagem
  para `subscriber.HandleMessage(ctx, msg)`.
- O handler decodifica o payload, chama os services e decide:
  - payload inválido (não adianta reprocessar) → `Ack`, logar e retornar o erro;
  - falha transitória (banco, rede, storage) → `Nack`, para a mensagem ser reentregue;
  - sucesso → `Ack`.
- Garanta que **todo** caminho de retorno faça `Ack` ou `Nack`.
- O processamento específico de cada formato fica em handlers dedicados (`handle_json.go`...).
- O processamento deve ser idempotente, porque a mesma mensagem pode chegar mais de uma vez.
- Feche os readers e recursos com `defer`.

## 11. Erros

- Os erros de domínio ficam em `internal/core/domain/errors.go`, como tipos com construtor:
  ```go
  type NotFoundError struct{ msg string }

  func (e *NotFoundError) Error() string { return e.msg }

  func NewNotFoundError(msg string) *NotFoundError { return &NotFoundError{msg: msg} }
  ```
- O repository traduz erros de infra para erros de domínio, e o handler traduz erros de domínio
  para status HTTP. Nenhuma camada intermediária conhece status HTTP.
- Inspecione erros com `errors.As`/`errors.Is` (nunca com type assertion direta) e adicione
  contexto com `fmt.Errorf("...: %w", err)`.
- Mensagens de erro em inglês, minúsculas e sem ponto final, descrevendo o que falhou.
- Nunca ignore erros (`x, _ := ...`), a não ser que seja intencional e esteja comentado.
- Use `panic`/`log.Fatal` apenas na inicialização, em `cmd/` e `adapters/`.

## 12. Estilo de código

- `gofmt`/`goimports` são obrigatórios. Agrupe os imports: primeiro a stdlib, depois os externos e os do projeto.
- O contexto é sempre o primeiro parâmetro e deve ser propagado até o banco ou o SDK.
- Receivers curtos, com a inicial do tipo (`o *Order`). Use ponteiro em services e repositories.
- Siglas em caixa alta (`ID`, `URL`, `JSON`, `HTTP`): `GetByID`, `CustomerID`, `SignedURL`.
- Funções pequenas, early return e nada de `else` depois de `return`.
- Se o método só repassa o erro, escreva `return o.repo.Save(ctx, order)` em vez de
  `if err != nil { return err }; return nil`.
- Código, identificadores, logs e mensagens de erro em inglês.
- Comentários só quando explicam o *porquê*.
- Use o pacote `log/slog` para logs estruturados, em vez de `fmt.Println`.

## 13. Testes

- Teste os services com mocks das interfaces de `repository`/`storage`.
- Teste os handlers com `httptest` e mocks das interfaces de `services/contracts.go`.
- Use table-driven tests, em arquivos `*_test.go` no mesmo pacote.
- Cubra os caminhos de erro: not found, validação e falha de dependência.
- Rode `go vet ./...` e `go test ./...` antes de considerar a tarefa concluída.

## 14. Infra e entrega

- `Dockerfile` com `go mod download` antes do `COPY . .`, para aproveitar o cache das camadas.
  Prefira multi-stage build com binário estático.
- `docker-compose.yml` com um serviço para cada dependência (banco etc.) e um para cada binário em
  `cmd/`, todos com `env_file: .env`.
- A migração é um binário separado (`cmd/migrate`) e nunca roda no startup da API.
- Mantenha o `go.mod` limpo com `go mod tidy`.
- README com contexto, tecnologias, como rodar e as rotas, com exemplos de request e response.

## 15. Git

- Uma branch por tarefa, com o nome do ID do ticket (ex.: `PROJ-123`), integrada via Pull Request.
- Commits pequenos e focados, com mensagem em inglês descrevendo a mudança
  (ex.: `add not found error to GetByID`). Evite mensagens genéricas como `fix` ou `new commit`.

---

## Checklist antes de entregar

- [ ] Nenhuma camada importa algo proibido pela tabela da seção 2.
- [ ] Services, handlers e subscribers dependem de interfaces.
- [ ] Nenhum `os.Getenv` fora de `adapters/`.
- [ ] Todo SQL usa placeholders, e não há `fmt.Sprintf` com dados de entrada em queries.
- [ ] `sql.ErrNoRows` é traduzido para erro de domínio, e erros são inspecionados com `errors.As`/`errors.Is`.
- [ ] Toda mensagem do worker recebe `Ack` ou `Nack` em todos os caminhos.
- [ ] Variáveis novas estão no `.env-sample`.
- [ ] `gofmt`, `go vet` e `go test` passam.
