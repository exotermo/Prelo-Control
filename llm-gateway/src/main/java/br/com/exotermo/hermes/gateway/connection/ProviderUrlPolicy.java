package br.com.exotermo.hermes.gateway.connection;

import java.net.InetAddress;
import java.net.URI;
import java.net.UnknownHostException;
import java.util.Locale;

/**
 * Canonical base URLs for the fixed providers, and SSRF validation for user-supplied
 * OpenAI-compatible ones. The gateway is on the internal network next to Postgres, so a base URL
 * that resolves to a loopback, private, link-local or otherwise local address is refused unless
 * the operator explicitly allowed private targets (e.g. a local Ollama).
 */
final class ProviderUrlPolicy {
    static final String ANTHROPIC = "anthropic";
    static final String OPENAI = "openai";
    static final String OPENAI_COMPATIBLE = "openai_compatible";

    private ProviderUrlPolicy() { }

    static boolean knownProvider(String provider) {
        return ANTHROPIC.equals(provider) || OPENAI.equals(provider) || OPENAI_COMPATIBLE.equals(provider);
    }

    static boolean requiresKey(String provider) {
        return !OPENAI_COMPATIBLE.equals(provider);
    }

    /** Returns the base URL to store/use: fixed for the two first-party providers. */
    static String resolveBaseUrl(String provider, String requested, boolean allowPrivate) {
        return switch (provider) {
            case ANTHROPIC -> "https://api.anthropic.com";
            case OPENAI -> "https://api.openai.com/v1";
            case OPENAI_COMPATIBLE -> validateCustom(requested, allowPrivate);
            default -> throw new ConnectionValidationException("provedor desconhecido");
        };
    }

    static String validateCustom(String raw, boolean allowPrivate) {
        if (raw == null || raw.isBlank()) throw new ConnectionValidationException("informe o endereço do serviço (base URL)");
        URI uri;
        try {
            uri = URI.create(raw.trim());
        } catch (IllegalArgumentException exception) {
            throw new ConnectionValidationException("endereço inválido");
        }
        String scheme = uri.getScheme() == null ? "" : uri.getScheme().toLowerCase(Locale.ROOT);
        if (uri.getHost() == null || uri.getUserInfo() != null || uri.getQuery() != null || uri.getFragment() != null) {
            throw new ConnectionValidationException("endereço inválido: use só esquema, host, porta e caminho");
        }
        if (!scheme.equals("https") && !(allowPrivate && scheme.equals("http"))) {
            throw new ConnectionValidationException("o endereço precisa usar https");
        }
        if (!allowPrivate) {
            try {
                for (InetAddress address : InetAddress.getAllByName(uri.getHost())) {
                    if (isLocal(address)) {
                        throw new ConnectionValidationException("endereços internos não são permitidos neste ambiente");
                    }
                }
            } catch (UnknownHostException exception) {
                throw new ConnectionValidationException("não foi possível resolver o endereço");
            }
        }
        String normalized = uri.toString();
        return normalized.endsWith("/") ? normalized.substring(0, normalized.length() - 1) : normalized;
    }

    static boolean isLocal(InetAddress address) {
        if (address.isLoopbackAddress() || address.isSiteLocalAddress() || address.isLinkLocalAddress()
            || address.isAnyLocalAddress() || address.isMulticastAddress()) {
            return true;
        }
        byte[] bytes = address.getAddress();
        // IPv6 unique-local fc00::/7 (Java's isSiteLocalAddress only covers deprecated fec0::/10).
        return bytes.length == 16 && (bytes[0] & 0xfe) == 0xfc;
    }
}
