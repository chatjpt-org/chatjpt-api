# ChatJPT API

Backend Go do ChatJPT. Esta primeira fatia fornece PostgreSQL, migrations,
criação administrativa de usuários, login, logout e sessão por cookie. Chat,
conversas e integração com o gateway de IA ainda não fazem parte da API.

## Segurança atual

- Senhas usam Argon2id com salt aleatório.
- O banco armazena somente SHA-256 de tokens de sessão, nunca o token do cookie.
- Cookies de sessão usam `HttpOnly`, `SameSite=Lax` e `Secure` em produção.
- Não existe endpoint de cadastro público. Usuários são criados pelo comando
  administrativo interativo, sem senha em argumento ou arquivo.

## Endpoints

| Método | Caminho | Descrição |
| --- | --- | --- |
| `GET` | `/healthz` | Verifica conexão com PostgreSQL |
| `POST` | `/v1/auth/login` | Cria sessão a partir de usuário e senha |
| `POST` | `/v1/auth/logout` | Revoga a sessão atual |
| `GET` | `/v1/auth/session` | Retorna o usuário da sessão atual |

## Desenvolvimento

O módulo requer Go 1.26. Defina `DATABASE_URL` antes de executar os comandos.

```bash
go test ./...
go vet ./...
staticcheck ./...
go run ./cmd/chatjpt-api migrate
go run ./cmd/chatjpt-api create-user
go run ./cmd/chatjpt-api serve
```

Para desenvolvimento sem HTTPS, use `JCHAT_COOKIE_SECURE=false`. Esse valor
nunca deve ser usado na KVM2 em produção.

## Docker na KVM2

Copie `infra/.env.example` para um arquivo `.env`, substitua os dois valores de
senha pelo mesmo segredo forte e crie a rede externa uma única vez:

```bash
docker network create jchat
docker compose up -d --build
```

O Compose mantém PostgreSQL fora da rede `jchat`. Apenas a API entra nessa rede,
para que o Caddy do repositório web a alcance pelo hostname `chatjpt-api`. A API não
publica portas no host. O Cloudflare Tunnel da KVM2 continuará apontando apenas
para o Caddy do cliente web.
