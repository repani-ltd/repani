ASCON known-answer test vectors

These files are the official reference Known-Answer Test (KAT) vectors for
Ascon-AEAD128 and Ascon-XOF128 (NIST SP 800-232), copied verbatim from the
canonical reference implementation repository:

.pre
  https://github.com/ascon/ascon-c

  LWC_AEAD_KAT_128_128.txt  crypto_aead/asconaead128/  (1089 vectors)
  LWC_XOF_KAT_128_512.txt   crypto_hash/asconxof128/   (1025 vectors)
  LWC_CXOF_KAT_128_512.txt  crypto_cxof/asconcxof128/  (1089 vectors)
.end

They are consumed by kat_test.go, which runs every vector against this
package's implementation. Do not edit them by hand; refresh only by
re-copying from upstream.
