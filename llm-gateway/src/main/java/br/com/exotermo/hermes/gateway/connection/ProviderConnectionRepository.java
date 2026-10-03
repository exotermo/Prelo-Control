package br.com.exotermo.hermes.gateway.connection;

import java.util.Optional;
import java.util.UUID;
import org.springframework.data.jpa.repository.JpaRepository;

interface ProviderConnectionRepository extends JpaRepository<ProviderConnection, UUID> {
    Optional<ProviderConnection> findFirstByScope(String scope);
    Optional<ProviderConnection> findByScopeAndProjectId(String scope, UUID projectId);
}
