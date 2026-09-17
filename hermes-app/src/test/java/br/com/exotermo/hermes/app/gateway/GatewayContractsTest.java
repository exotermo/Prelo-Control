package br.com.exotermo.hermes.app.gateway;

import static org.junit.jupiter.api.Assertions.assertEquals;
import br.com.exotermo.hermes.app.gateway.GatewayContracts.Message;
import org.junit.jupiter.api.Test;

class GatewayContractsTest {
    @Test void keepsTheGatewayContractIndependentFromAnyProviderSdk() {
        var message = new Message("user", "ping");
        assertEquals("user", message.role());
        assertEquals("ping", message.content());
    }
}
