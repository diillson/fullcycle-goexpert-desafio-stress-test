# Load Test Tool
    
    Uma ferramenta de linha de comando em Go para realizar testes de carga em endpoints HTTP.
    
## Características
    
    - Execução de múltiplas requisições concorrentes
    - Métricas detalhadas de performance
    - Múltiplos formatos de saída (plain, JSON, CSV)
    - Suporte a diferentes métodos HTTP
    - Análise estatística completa (média, mínimo, máximo, percentis)
    - Monitoramento de progresso em tempo real
    
## Instalação
    
```bash
    git clone https://github.com/diillson/fullcycle-goexpert-desafio-stress-test
```

## Uso

### Comando Básico

    go run main.go -url=https://api.exemplo.com -requests=100 -concurrency=10

### Parâmetros Disponíveis

•  -url : URL do endpoint a ser testado (obrigatório)
•  -requests : Número total de requisições (obrigatório)
•  -concurrency : Número de requisições simultâneas (default: 1)
•  -timeout : Timeout para cada requisição (default: 10s)
•  -method : Método HTTP (default: GET)
•  -format : Formato de saída (plain, json, csv) (default: plain)

### Exemplos

1. Teste simples com 100 requisições:

        go run main.go -url=https://api.exemplo.com -requests=100

2. Teste com alta concorrência:

       go run main.go -url=https://api.exemplo.com -requests=1000 -concurrency=50

3. Exportar resultados em JSON:

       go run main.go -url=https://api.exemplo.com -requests=100 -format=json

## Exemplos por Tipo de Requisição

### Teste GET Básico

    go run main.go -url "https://example.com" -requests 100 -concurrency 10

### Teste POST com Dados JSON

    go run main.go \
      -url "https://api.example.com/login" \
      -method "POST" \
      -requests 200 \
      -concurrency 20 \
      -headers "Content-Type:application/json" \
      -body '{"username":"testuser","password":"password123"}'

### Teste com Autenticação

    go run main.go \
      -url "https://api.example.com/protected-endpoint" \
      -method "GET" \
      -requests 300 \
      -concurrency 30 \
      -headers "Authorization:Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." \
      -timeout 8s

### Teste PUT para Atualização de Recursos

    go run main.go \
      -url "https://api.example.com/users/123" \
      -method "PUT" \
      -requests 150 \
      -concurrency 15 \
      -headers "Content-Type:application/json,Authorization:Bearer token123" \
      -body '{"name":"Updated User","status":"active"}'

### Teste DELETE

    go run main.go \
      -url "https://api.example.com/resources/456" \
      -method "DELETE" \
      -requests 100 \
      -concurrency 10 \
      -headers "Authorization:Bearer token123"

### Exportando Resultados em Diferentes Formatos

#### CSV

    go run main.go \
      -url "https://example.com/api" \
      -requests 500 \
      -concurrency 25 \
      -format csv > results.csv

#### JSON

    go run main.go \
      -url "https://example.com/api" \
      -requests 500 \
      -concurrency 25 \
      -format json > results.json

## Teste de Estresse com Alto Volume

    go run main.go \
      -url "https://api.example.com/endpoint" \
      -requests 10000 \
      -concurrency 200 \
      -timeout 30s

### usando docker

Build:
```bash
docker build -t loadtest .
```

Execução:
```bash
    docker run loadtest \                                                    
    -url=https://google.com \
    -requests=1000 \
    -concurrency=10 \
    -timeout=5s \
    -format=json \
    -method=GET
```


## Saída

A ferramenta fornece um relatório detalhado incluindo:

• Tempo total de execução
• Requisições por segundo (RPS)
• Estatísticas de tempo de resposta
• Mínimo
• Máximo
• Média
• Percentis (P50, P90, P95, P99)
• Distribuição de códigos de status
• Detalhes de erros (se houver)

### Exemplo de Saída

    Progress: 100.0% (1000/1000) | Rate: 78.74 req/s

    📊 Test Results Summary
    ----------------------------------------
    Total Time: 12.70 seconds
    Total Requests: 1000
    Requests per Second: 78.73
    ----------------------------------------

    ⚡ Response Time Stats
    ----------------------------------------
    Minimum: 1.233792ms
    Maximum: 881.267667ms
    Average: 126.945993ms
    P50: 3.542625ms
    P90: 573.509125ms
    P95: 596.011042ms
    P99: 632.607084ms
    ----------------------------------------

    📈 Status Code Distribution
    ----------------------------------------
    Status 0: 757 requests (75.7%)
    Status 200: 243 requests (24.3%)
    ----------------------------------------

    ❌ Errors: 757 (75.7%)

    ❌ Error Details:
    ----------------------------------------
    Get "https://google.com": dial tcp 142.251.133.174:443: connect: connection refused: 755 occurrences (75.5%)
    Get "https://www.google.com/": dial tcp 142.250.78.228:443: connect: connection refused: 2 occurrences (0.2%)

## Requisitos

• Go 1.24 ou superior
• Acesso à Internet (para testar endpoints externos)