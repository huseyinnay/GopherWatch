# GopherWatch 

GopherWatch, uygulamalarınızın (HTTP/TCP) sağlık durumlarını sürekli izleyen, başarısızlık durumunda ilgili Docker konteynerlerini otomatik olarak yeniden başlatan ve Slack, Discord veya Telegram üzerinden anlık bildirimler gönderen, Go diliyle yazılmış modern, hafif ve proaktif bir arka plan (daemon) servisidir.

## Öne Çıkan Özellikler

- **Çoklu Hedef İzleme (Probing):** HTTP (GET, POST vs.) ve TCP endpoint'lerinizi belirlediğiniz aralıklarla izler.
- **Otomatik İyileştirme (Auto-Restart):** Bir hedef belirlenen eşik (failure_threshold) kadar üst üste başarısız olursa, ona bağlı olan Docker konteynerini Docker API üzerinden otomatik olarak yeniden başlatır.
- **Esnek Bildirim Sistemi:** Konteyner yeniden başlatıldığında veya bir hedef sağlıklı/sağlıksız duruma geçtiğinde Slack, Discord ve Telegram webhook'ları üzerinden anlık bildirim gönderir.
- **Kesintisiz Konfigürasyon Yenileme (Hot-Reload):** Servisi kapatmadan `gopherwatch reload` komutuyla ayarlarınızı canlı olarak güncelleyebilirsiniz.
- **HTTP API ve Dashboard:** `8090` portu üzerinden güncel durumları, son olayları (events) görebilir ve yönetebilirsiniz.
- **Minimal ve Dağıtımı Kolay:** Sadece tek bir derlenmiş binary (veya minimal Docker imajı) olarak çalışır.
- **Olay Günlükleri (Event Logs):** Yapılan her bir işlemi ve durum değişikliklerini JSONL formatında kaydeder.

---

## Mimari ve Bileşenler

Proje modüler bir mimariyle tasarlanmıştır:
- **Prober:** Hedefleri (HTTP/TCP) periyodik olarak yoklar.
- **Tracker & Store:** Hedeflerin son sağlık durumlarını, ardışık hata sayılarını ve son kontrol zamanlarını bellekte tutar.
- **Reactor:** Hedef `UNHEALTHY` durumuna düştüğünde tepki verir, `Docker` modülüne yeniden başlatma emri gönderir.
- **Docker:** Sistemdeki Docker daemon'u ile konuşarak konteyner işlemlerini yapar.
- **Notifier:** Çeşitli kanallardan (Slack, Discord, Telegram) bildirim gönderir.
- **HTTP API:** Gopherwatch'ın durumunu dışarıya sunan web arayüzü ve API katmanıdır.
- **Supervisor:** Tüm bu süreçleri arka planda daemon olarak yönetir.

---

## Konfigürasyon (`config.yaml`)

Ayarlarınızı `configs/gopherwatch.yaml` dosyası üzerinden yapabilirsiniz:

```yaml
global:
  log_level: info
  check_interval: 10s
  failure_threshold: 3
  restart_cooldown: 60s
  event_log_file: "/var/lib/gopherwatch/events.jsonl"

http:
  enabled: true
  addr: 0.0.0.0:8090
  auth_token: "your-secret-token"

targets:
  - name: my-api
    type: http
    url: http://localhost:8080/health
    method: GET
    expected_status: [200]
    timeout: 5s
    container: my-api-container  # Hata durumunda restart edilecek konteyner
    restart_cooldown: 30s
    
  - name: redis-cache
    type: tcp
    address: localhost:6379
    container: redis

notifications:
  rate_limit: 60s
  discord:
    enabled: false
    webhook_url: https://discord.com/api/webhooks/...
  telegram:
    enabled: false
    bot_token: "..."
    chat_id: "..."
  slack:
    enabled: false
    webhook_url: https://hooks.slack.com/services/...
```

---

## Başlangıç ve Kurulum

Projeyi canlıya almak veya kendi bilgisayarınızda arka plan servisi olarak çalıştırmak için aşağıdaki 3 yöntemden birini seçebilirsiniz.

### Seçenek 1: Doğrudan Çalıştırma (Binary)

**1. Derleme:**
```bash
go build -o gopherwatch ./cmd/gopherwatch
```

**2. Daemon'u Başlatma:**
```bash
./gopherwatch start -config configs/gopherwatch.yaml
```
*(Bu komut arka planda `/tmp/gopherwatch.pid` oluşturur ve izleme sistemini başlatır)*

**3. Durumu Görüntüleme:**
```bash
./gopherwatch status -config configs/gopherwatch.yaml
```

**4. Ayarları Kesintisiz Yenileme (Hot-reload):**
```bash
# configs/gopherwatch.yaml dosyasında değişiklik yapın ve ardından:
./gopherwatch reload
```

### Seçenek 2: Docker Üzerinden Çalıştırma

Gopherwatch, hedeflediği konteynerleri yeniden başlatabilmek için `docker.sock` dosyasına erişim gerektirir.

**1. İmajı Oluşturun:**
```bash
docker build -t gopherwatch .
```

**2. Konteyneri Başlatın:**
```bash
docker run -d \
  --name gopherwatch-daemon \
  -p 8090:8090 \
  -v $(pwd)/configs/gopherwatch.yaml:/etc/gopherwatch/config.yaml \
  -v $(pwd)/events.jsonl:/app/events.jsonl \
  -v /var/run/docker.sock:/var/run/docker.sock \
  gopherwatch
```

### Seçenek 3: Linux Systemd Servisi Olarak Kurulum

Sunucularda arka planda servis olarak en stabil çalışma yöntemidir.

**1. Derleyip sisteme taşıyın:**
```bash
go build -o gopherwatch ./cmd/gopherwatch
sudo mv gopherwatch /usr/local/bin/
```

**2. Config ve log dizinlerini oluşturun:**
```bash
sudo mkdir -p /etc/gopherwatch
sudo cp configs/gopherwatch.yaml /etc/gopherwatch/config.yaml
```

**3. Service dosyasını aktifleştirin:**
```bash
sudo cp deploy/gopherwatch.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now gopherwatch
```

**4. Servisi izleyin:**
```bash
sudo systemctl status gopherwatch
sudo journalctl -fu gopherwatch
```

---

## Lisans
Bu proje açık kaynaklıdır. Gereksinimlerinize göre dilediğiniz gibi düzenleyip kullanabilirsiniz.
