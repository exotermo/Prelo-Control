package br.com.exotermo.prelo.gateway.provider;

/** A 429 that is not "slow down" but "no credit left" (OpenAI insufficient_quota, billing). */
public class ProviderQuotaExceededException extends ProviderRateLimitedException {
    public ProviderQuotaExceededException(String message) { super(message); }
}
