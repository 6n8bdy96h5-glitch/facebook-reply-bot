# Facebook Reply Bot

بوت Messenger مكتوب بلغة Go باستخدام Gin. يستقبل رسائل الصفحة عبر Webhook، يرسل ردًا تلقائيًا، ويرسل إشعارًا إلى Gmail عبر Resend HTTPS. يبقى SMTP خيارًا احتياطيًا للتشغيل المحلي أو خطط Render المدفوعة.

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
- `RESEND_API_KEY`
- `NOTIFY_EMAIL`

لا ترفع `.env` إلى GitHub. الملف مستبعد بواسطة `.gitignore`.

يستخدم النشر المجاني `Messenger Bot <onboarding@resend.dev>` كمرسل، ولذلك يجب أن يطابق `NOTIFY_EMAIL` بريد حساب Resend إلى أن يتم توثيق نطاق مخصص.

بعد النشر استخدم رابط Webhook التالي في Meta:

```text
https://<render-service>.onrender.com/webhook
```
