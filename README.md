# Bot de Trading para Binance

Este bot implementa una estrategia de scalping cuantitativo en Binance, con el objetivo de realizar un mínimo de 100 operaciones diarias, cada una generando un margen neto mínimo de 1 USD, y reinvirtiendo automáticamente las ganancias.

## Características

- **Scalping Cuantitativo**: Evalúa en tiempo real todos los pares disponibles en Binance para identificar oportunidades basadas en volatilidad, volumen y spreads mínimos.
- **Reinversión Dinámica**: Ajusta el capital operativo conforme crece el balance.
- **Gestión de Riesgos**: Implementa stop-loss y take-profit automáticos.
- **Aprendizaje Adaptativo**: Utiliza técnicas de Q-Learning para mejorar la toma de decisiones.
- **Arquitectura Hexagonal**: Separa el dominio central de las dependencias externas.

## Estructura del Proyecto

El proyecto sigue una arquitectura hexagonal (puertos y adaptadores):

```
├── cmd/                # Punto de entrada de la aplicación
│   └── bot/            # Aplicación principal
├── domain/             # Dominio de la aplicación
│   ├── core/           # Lógica de negocio
│   └── ports/          # Interfaces para los adaptadores
├── infrastructure/     # Implementaciones concretas
│   ├── adapters/       # Adaptadores genéricos
│   └── binance/        # Adaptadores específicos para Binance
└── pkg/                # Utilidades compartidas
```

## Requisitos

- Go 1.22 o superior
- Cuenta en Binance con API Key y Secret

## Configuración

1. Copia el archivo `.env.example` a `.env` y configura:
   - Credenciales de la API de Binance
   - Parámetros de trading (objetivo diario, spread mínimo, etc.)
   - Parámetros de gestión de riesgos

```
# Credenciales API Binance
API_KEY=tu_api_key
API_SECRET=tu_api_secret

# Configuración Trading
MIN_PROFIT_USD=1.0
DAILY_TRADE_TARGET=100
REINVESTMENT_PERCENTAGE=100
MAX_SIMULTANEOUS_TRADES=5
TRADE_SIZE_PERCENTAGE=2.0

# Stop Loss y Take Profit
STOP_LOSS_PERCENTAGE=0.5
TAKE_PROFIT_PERCENTAGE=1.2

# Configuración de Trading
PAIRS_TO_ANALYZE=BTCUSDT,ETHUSDT,BNBUSDT,XRPUSDT,ADAUSDT,DOGEUSDT,SOLUSDT,LTCUSDT,DOTUSDT,AVAXUSDT
VOLATILITY_THRESHOLD=0.5
VOLUME_THRESHOLD=100000
MIN_SPREAD_PERCENTAGE=0.05
```

## Instalación

```bash
# Clonar el repositorio
git clone https://github.com/tu-usuario/binancebot.git
cd binancebot

# Instalar dependencias
go mod download

# Construir el proyecto
go build -o bin/bot cmd/bot/main.go
```

## Uso

```bash
# Iniciar el bot
./bin/bot
```

Para detener el bot de forma segura, usa `Ctrl+C`.

## Componentes Principales

### Data Analysis
Evalúa constantemente los pares de Binance para detectar oportunidades de scalping.

### Order Execution
Ejecuta operaciones de forma rápida y segura.

### Risk Management
Gestiona el riesgo y protege el capital.

### Reinvestment
Reinvierte las ganancias automáticamente para escalar la operación.

### Learning Module
Permite que el bot aprenda y optimice su estrategia a partir de resultados históricos.

## Advertencia

El trading de criptomonedas conlleva riesgos significativos. Este bot es experimental y no garantiza beneficios. Úsalo bajo tu propia responsabilidad y solo con capital que estés dispuesto a perder.

## Licencia

MIT 