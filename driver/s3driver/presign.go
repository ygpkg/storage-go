package s3driver

import (
	"context"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// contentTypePresigner 是「预签名 PUT 绑定 Content-Type」专用签名器。
//
// 为什么需要它：aws-sdk-go-v2 在 PutObject 的 presign 路径上挂了
// awshttp.RemoveContentTypeHeader，签名前会把 Content-Type 头摘掉，
// 于是预签名 URL 只覆盖 host——上传方即使传了别的类型，服务端也无从校验。
// 当调用方显式要求绑定时（storage.WithPresignBindContentType），
// 本签名器在被摘掉之后再把 Content-Type 写回待签名请求，使其进入 signed headers。
//
// 两个实现要点：
//  1. DisableURIPathEscaping 必须打开：S3 客户端默认签名器就是这么设的
//     （aws-sdk-go-v2 的 s3.Options 里 `so.DisableURIPathEscaping = true`）。
//     裸 v4.Signer 不设，会对已转义的 path 再 EscapePath 一次（%E5 → %25E5），
//     含中文/空格的 key 在 COS 上必然 SignatureDoesNotMatch。
//  2. 只额外加 Content-Type，不改变其余签名输入（方法与原签名器一致）。
type contentTypePresigner struct {
	contentType string
	inner       *v4.Signer
}

// newContentTypePresigner 构造绑定 Content-Type 的 presigner（单次转义）。
func newContentTypePresigner(contentType string) contentTypePresigner {
	return contentTypePresigner{
		contentType: contentType,
		inner:       v4.NewSigner(func(o *v4.SignerOptions) { o.DisableURIPathEscaping = true }),
	}
}

// PresignHTTP 实现 aws.HTTPPresignerV4。
func (p contentTypePresigner) PresignHTTP(
	ctx context.Context, creds aws.Credentials, r *http.Request,
	payloadHash, service, region string, signingTime time.Time, optFns ...func(*v4.SignerOptions),
) (string, http.Header, error) {
	if p.contentType != "" {
		r.Header.Set("Content-Type", p.contentType)
	}
	return p.inner.PresignHTTP(ctx, creds, r, payloadHash, service, region, signingTime, optFns...)
}
