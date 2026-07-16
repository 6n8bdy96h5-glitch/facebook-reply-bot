# Facebook Reply Bot

بوت Messenger مكتوب بلغة Go باستخدام Gin. يستقبل رسائل الصفحة عبر Webhook، يرسل ردًا تلقائيًا، ويرسل إشعارًا إلى Gmail.

## التشغيل المحلي

1. انسخ `.env.example` إلى `.env`.
2. ضع القيم السرية محليًا فقط.
3. شغّل:

```bash
go run .
```

## الاختبارات

```bash
go test ./...
go vet ./...
```

## النشر على Render

المشروع يحتوي `render.yaml` بإعدادات البناء والتشغيل ونقطة فحص الصحة. عند إنشاء Blueprint أو Web Service في Render، أدخل القيم السرية التالية من ملف `.env` المحلي:

- `VERIFY_TOKEN`
- `PAGE_ACCESS_TOKEN`
- `SMTP_PASSWORD`

لا ترفع `.env` إلى GitHub. الملف مستبعد بواسطة `.gitignore`.

بعد النشر استخدم رابط Webhook التالي في Meta:

```text
https://<render-service>.onrender.com/webhook
```
