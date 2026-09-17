package br.com.exotermo.hermes.gateway.observability;

import java.util.UUID;
import java.util.regex.Pattern;
import org.springframework.http.HttpStatus;
import org.springframework.web.server.ResponseStatusException;

public final class RequestIdPolicy {
    public static final int MAX_LENGTH = 100;
    private static final Pattern SAFE_VALUE = Pattern.compile("[A-Za-z0-9._-]{1," + MAX_LENGTH + "}");
    private RequestIdPolicy() { }

    public static String resolve(String value) {
        if (value == null) return UUID.randomUUID().toString();
        if (!SAFE_VALUE.matcher(value).matches()) {
            throw new ResponseStatusException(HttpStatus.BAD_REQUEST, "X-Request-Id must contain 1 to 100 letters, digits, dots, underscores, or hyphens");
        }
        return value;
    }
}
