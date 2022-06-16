package certificate

import (
	"testing"
	time "time"

	tassert "github.com/stretchr/testify/assert"

	"github.com/openservicemesh/osm/pkg/announcements"
	"github.com/openservicemesh/osm/pkg/certificate/pem"
	"github.com/openservicemesh/osm/pkg/messaging"
)

func TestRotor(t *testing.T) {
	assert := tassert.New(t)

	cnPrefix := "foo"
	validityPeriod := -1 * time.Hour // negative time means this cert has already expired -- will be rotated asap

	stop := make(chan struct{})
	defer close(stop)
	msgBroker := messaging.NewBroker(stop)
	certManager, err := NewManager(&fakeMRCClient{}, validityPeriod, msgBroker)
	certManager.Start(5*time.Second, stop)
	assert.NoError(err)

	certA, err := certManager.IssueCertificate(cnPrefix, WithValidityPeriod(validityPeriod))
	assert.NoError(err)
	certRotateChan := msgBroker.GetCertPubSub().Sub(announcements.CertificateRotated.String())

	// Wait for two certificate rotations to be announced and terminate
	<-certRotateChan
	newCert, err := certManager.IssueCertificate(cnPrefix, WithValidityPeriod(validityPeriod))
	assert.NoError(err)
	assert.NotEqual(certA.GetExpiration(), newCert.GetExpiration())
	assert.NotEqual(certA, newCert)
}

func TestReleaseCertificate(t *testing.T) {
	cn := "Test CN"
	cert := &Certificate{
		CommonName: CommonName(cn),
		Expiration: time.Now().Add(1 * time.Hour),
	}

	manager := &Manager{}
	manager.cache.Store(cn, cert)

	testCases := []struct {
		name     string
		cnPrefix string
	}{
		{
			name:     "release existing certificate",
			cnPrefix: cn,
		},
		{
			name:     "release non-existing certificate",
			cnPrefix: cn,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert := tassert.New(t)

			manager.ReleaseCertificate(tc.cnPrefix)
			cert := manager.getFromCache(tc.cnPrefix)

			assert.Nil(cert)
		})
	}
}

func TestListIssuedCertificate(t *testing.T) {
	assert := tassert.New(t)

	cn := CommonName("Test Cert")
	cert := &Certificate{
		CommonName: cn,
	}

	anotherCn := CommonName("Another Test Cert")
	anotherCert := &Certificate{
		CommonName: anotherCn,
	}

	expectedCertificates := []*Certificate{cert, anotherCert}

	manager := &Manager{}
	manager.cache.Store(cn, cert)
	manager.cache.Store(anotherCn, anotherCert)

	cs := manager.ListIssuedCertificates()
	assert.Len(cs, 2)

	for i, c := range cs {
		match := false
		for _, ec := range expectedCertificates {
			if c.GetCommonName() == ec.GetCommonName() {
				match = true
				assert.Equal(ec, c)
				break
			}
		}

		if !match {
			t.Fatalf("Certificate #%v %v does not exist", i, c.GetCommonName())
		}
	}
}

func TestIssueCertificate(t *testing.T) {
	cnPrefix := "fake-cert-cn"
	assert := tassert.New(t)

	t.Run("single key issuer", func(t *testing.T) {
		cm := &Manager{
			// The root certificate signing all newly issued certificates
			signingIssuer:               &issuer{ID: "id1", Issuer: &fakeIssuer{id: "id1"}, CertificateAuthority: pem.RootCertificate("id1"), TrustDomain: "fake1.domain.com"},
			validatingIssuer:            &issuer{ID: "id1", Issuer: &fakeIssuer{id: "id1"}, CertificateAuthority: pem.RootCertificate("id1"), TrustDomain: "fake2.domain.com"},
			serviceCertValidityDuration: time.Hour,
		}
		// single signingIssuer, not cached
		cert1, err := cm.IssueCertificate(cnPrefix)
		assert.NoError(err)
		assert.NotNil(cert1)
		assert.Equal(cert1.signingIssuerID, "id1")
		assert.Equal(cert1.validatingIssuerID, "id1")
		assert.Equal(cert1.GetIssuingCA(), pem.RootCertificate("id1"))
		assert.Equal(CommonName("fake-cert-cn.fake1.domain.com"), cert1.GetCommonName())

		// single keyIssuer cached
		cert2, err := cm.IssueCertificate(cnPrefix)
		assert.NoError(err)
		assert.Equal(cert1, cert2)
		assert.Equal(CommonName("fake-cert-cn.fake1.domain.com"), cert1.GetCommonName())

		// single key issuer, old version cached
		// TODO: could use informer logic to test mrc updates instead of just manually making changes.
		cm.signingIssuer = &issuer{ID: "id2", Issuer: &fakeIssuer{id: "id2"}, CertificateAuthority: pem.RootCertificate("id2"), TrustDomain: "fake2.domain.com"}
		cm.validatingIssuer = &issuer{ID: "id2", Issuer: &fakeIssuer{id: "id2"}, CertificateAuthority: pem.RootCertificate("id2")}

		cert3, err := cm.IssueCertificate(cnPrefix)
		assert.Equal(CommonName("fake-cert-cn.fake2.domain.com"), cert3.GetCommonName())

		assert.NoError(err)
		assert.NotNil(cert3)
		assert.Equal(cert3.signingIssuerID, "id2")
		assert.Equal(cert3.validatingIssuerID, "id2")
		assert.NotEqual(cert2, cert3)
		assert.Equal(cert3.GetIssuingCA(), pem.RootCertificate("id2"))
	})

	t.Run("2 issuers", func(t *testing.T) {
		cm := &Manager{
			// The root certificate signing all newly issued certificates
			signingIssuer:               &issuer{ID: "id1", Issuer: &fakeIssuer{id: "id1"}, CertificateAuthority: pem.RootCertificate("id1"), TrustDomain: "fake1.domain.com"},
			validatingIssuer:            &issuer{ID: "id2", Issuer: &fakeIssuer{id: "id2"}, CertificateAuthority: pem.RootCertificate("id2"), TrustDomain: "fake2.domain.com"},
			serviceCertValidityDuration: time.Hour,
		}

		// Not cached
		cert1, err := cm.IssueCertificate(cnPrefix)
		assert.NoError(err)
		assert.NotNil(cert1)
		assert.Equal(cert1.signingIssuerID, "id1")
		assert.Equal(cert1.validatingIssuerID, "id2")
		assert.Equal(pem.RootCertificate("id1"), cert1.GetIssuingCA())
		assert.Equal(pem.RootCertificate("id1id2"), cert1.GetTrustedCAs())
		assert.Equal(CommonName("fake-cert-cn.fake1.domain.com"), cert1.GetCommonName())

		// cached
		cert2, err := cm.IssueCertificate(cnPrefix)
		assert.NoError(err)
		assert.Equal(cert1, cert2)
		assert.Equal(CommonName("fake-cert-cn.fake1.domain.com"), cert2.GetCommonName())

		// cached, but validatingIssuer is removed
		cm.validatingIssuer = cm.signingIssuer
		cert3, err := cm.IssueCertificate(cnPrefix)
		assert.NoError(err)
		assert.NotEqual(cert1, cert3)
		assert.Equal(cert3.signingIssuerID, "id1")
		assert.Equal(cert3.validatingIssuerID, "id1")
		assert.Equal(cert3.GetIssuingCA(), pem.RootCertificate("id1"))
		assert.Equal(CommonName("fake-cert-cn.fake1.domain.com"), cert1.GetCommonName())

		// cached, but signingIssuer is old
		cm.signingIssuer = &issuer{ID: "id2", Issuer: &fakeIssuer{id: "id2"}, CertificateAuthority: pem.RootCertificate("id2"), TrustDomain: "fake2.domain.com"}
		cert4, err := cm.IssueCertificate(cnPrefix)
		assert.NoError(err)
		assert.NotEqual(cert3, cert4)
		assert.Equal(cert4.signingIssuerID, "id2")
		assert.Equal(cert4.validatingIssuerID, "id1")
		assert.Equal(pem.RootCertificate("id2"), cert4.GetIssuingCA())
		assert.Equal(pem.RootCertificate("id2id1"), cert4.GetTrustedCAs())
		assert.Equal(CommonName("fake-cert-cn.fake2.domain.com"), cert4.GetCommonName())

		// cached, but validatingIssuer is old
		cm.validatingIssuer = &issuer{ID: "id3", Issuer: &fakeIssuer{id: "id3"}, CertificateAuthority: pem.RootCertificate("id3"), TrustDomain: "fake3.domain.com"}
		cert5, err := cm.IssueCertificate(cnPrefix)
		assert.NoError(err)
		assert.NotEqual(cert4, cert5)
		assert.Equal(cert5.signingIssuerID, "id2")
		assert.Equal(cert5.validatingIssuerID, "id3")
		assert.Equal(pem.RootCertificate("id2"), cert5.GetIssuingCA())
		assert.Equal(pem.RootCertificate("id2id3"), cert5.GetTrustedCAs())
		assert.Equal(CommonName("fake-cert-cn.fake2.domain.com"), cert5.GetCommonName())
	})

	t.Run("bad issuers", func(t *testing.T) {
		cm := &Manager{
			// The root certificate signing all newly issued certificates
			signingIssuer:               &issuer{ID: "id1", Issuer: &fakeIssuer{id: "id1", err: true}, CertificateAuthority: pem.RootCertificate("id1")},
			validatingIssuer:            &issuer{ID: "id2", Issuer: &fakeIssuer{id: "id2", err: true}, CertificateAuthority: pem.RootCertificate("id2")},
			serviceCertValidityDuration: time.Hour,
		}

		// bad signingIssuer
		cert, err := cm.IssueCertificate(cnPrefix)
		assert.Nil(cert)
		assert.EqualError(err, "id1 failed")

		// bad validatingIssuer (should still succeed)
		cm.signingIssuer = &issuer{ID: "id3", Issuer: &fakeIssuer{id: "id3"}, CertificateAuthority: pem.RootCertificate("id3")}
		cert, err = cm.IssueCertificate(cnPrefix)
		assert.NoError(err)
		assert.Equal(cert.signingIssuerID, "id3")
		assert.Equal(cert.validatingIssuerID, "id2")
		assert.Equal(pem.RootCertificate("id3"), cert.GetIssuingCA())
		assert.Equal(pem.RootCertificate("id3id2"), cert.GetTrustedCAs())

		// insert a cached cert
		cm.validatingIssuer = cm.signingIssuer
		cert, err = cm.IssueCertificate(cnPrefix)
		assert.NoError(err)
		assert.NotNil(cert)

		// bad signing cert on an existing cached cert, because the signingIssuer is new
		cm.signingIssuer = &issuer{ID: "id1", Issuer: &fakeIssuer{id: "id1", err: true}, CertificateAuthority: pem.RootCertificate("id1")}
		cert, err = cm.IssueCertificate(cnPrefix)
		assert.EqualError(err, "id1 failed")
		assert.Nil(cert)
	})
}
