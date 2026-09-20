package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/agmonetti/lazyteams/internal/fabricprobe"
	"github.com/agmonetti/lazyteams/internal/graph"
)

type resolvedTarget struct {
	target    fabricprobe.Target
	threadID  string
	auditUser string
	teamName  string
	chanName  string
	userName  string
	fromRole  string
	toRole    string
}

func promptChoice(scanner *bufio.Scanner, out io.Writer, prompt string, min, max int) (int, error) {
	for {
		fmt.Fprint(out, prompt)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		text := strings.TrimSpace(scanner.Text())
		if text == "" && min == 1 && max == 1 {
			return 1, nil
		}
		num, err := strconv.Atoi(text)
		if err == nil && num >= min && num <= max {
			return num, nil
		}
		fmt.Fprintf(out, "Opción inválida. Por favor ingresá un número entre %d y %d:\n", min, max)
	}
}

func resolveTargetInteractive(c *graph.Client, in io.Reader, out io.Writer) (*resolvedTarget, error) {
	scanner := bufio.NewScanner(in)

	fmt.Fprintln(out, "Obteniendo tus equipos de Microsoft Teams...")
	teams, err := c.GetJoinedTeams()
	if err != nil {
		return nil, fmt.Errorf("error al obtener equipos: %w", err)
	}
	if len(teams) == 0 {
		return nil, errors.New("no pertenecés a ningún equipo")
	}

	var selectedTeam graph.Team
	if len(teams) == 1 {
		selectedTeam = teams[0]
		fmt.Fprintf(out, "✓ Equipo detectado: %s\n", selectedTeam.DisplayName)
	} else {
		fmt.Fprintln(out, "\nEquipos disponibles:")
		for i, t := range teams {
			fmt.Fprintf(out, "  [%d] %s\n", i+1, t.DisplayName)
		}
		choice, err := promptChoice(scanner, out, fmt.Sprintf("Seleccioná un equipo [1-%d]: ", len(teams)), 1, len(teams))
		if err != nil {
			return nil, err
		}
		selectedTeam = teams[choice-1]
	}

	fmt.Fprintf(out, "\nBuscando canales en %s...\n", selectedTeam.DisplayName)
	channels, err := c.GetChannels(selectedTeam.ID)
	if err != nil {
		return nil, fmt.Errorf("error al obtener canales: %w", err)
	}

	var privateChannels []graph.Channel
	for _, ch := range channels {
		if strings.EqualFold(ch.MembershipType, "private") {
			privateChannels = append(privateChannels, ch)
		}
	}
	if len(privateChannels) == 0 {
		return nil, fmt.Errorf("el equipo %q no tiene canales privados (Fabric requiere un canal privado)", selectedTeam.DisplayName)
	}

	var selectedChannel graph.Channel
	if len(privateChannels) == 1 {
		selectedChannel = privateChannels[0]
		fmt.Fprintf(out, "✓ Canal privado detectado: %s\n", selectedChannel.DisplayName)
	} else {
		fmt.Fprintln(out, "\nCanales privados disponibles:")
		for i, ch := range privateChannels {
			fmt.Fprintf(out, "  [%d] %s\n", i+1, ch.DisplayName)
		}
		choice, err := promptChoice(scanner, out, fmt.Sprintf("Seleccioná un canal privado [1-%d]: ", len(privateChannels)), 1, len(privateChannels))
		if err != nil {
			return nil, err
		}
		selectedChannel = privateChannels[choice-1]
	}

	fmt.Fprintf(out, "\nResolviendo identificadores del canal %s...\n", selectedChannel.DisplayName)
	substrateID, err := c.GetChannelSubstrateID(selectedChannel.ID)
	if err != nil {
		return nil, fmt.Errorf("error al obtener substrate ID del canal: %w", err)
	}

	tenantID := c.GetTenantID()
	if tenantID == "" {
		creds := c.Credentials()
		if creds.Web.Tenant != "" {
			tenantID = creds.Web.Tenant
		} else if creds.Graph.Tenant != "" {
			tenantID = creds.Graph.Tenant
		}
	}
	if tenantID == "" {
		return nil, errors.New("no se pudo determinar el tenant ID")
	}

	fmt.Fprintf(out, "Obteniendo miembros de %s...\n", selectedChannel.DisplayName)
	members, err := c.GetChannelMembers(selectedChannel.ID)
	if err != nil {
		return nil, fmt.Errorf("error al obtener miembros del canal: %w", err)
	}
	if len(members) == 0 {
		return nil, errors.New("no se encontraron miembros en el canal")
	}

	var selectedMember graph.TeamMember
	if len(members) == 1 {
		selectedMember = members[0]
		fmt.Fprintf(out, "✓ Miembro único detectado: %s (rol actual: %s)\n", selectedMember.DisplayName, selectedMember.Role)
	} else {
		fmt.Fprintln(out, "\nMiembros del canal:")
		for i, m := range members {
			toRole := "Member"
			if m.Role != "Owner" {
				toRole = "Owner"
			}
			fmt.Fprintf(out, "  [%d] %s (rol actual: %s -> simular cambio a: %s)\n", i+1, m.DisplayName, m.Role, toRole)
		}
		choice, err := promptChoice(scanner, out, fmt.Sprintf("Seleccioná un miembro [1-%d]: ", len(members)), 1, len(members))
		if err != nil {
			return nil, err
		}
		selectedMember = members[choice-1]
	}

	targetRoleNum := 1
	fromRole := selectedMember.Role
	toRole := "Member"
	if selectedMember.Role != "Owner" {
		targetRoleNum = 2
		toRole = "Owner"
	}

	channelOID := fmt.Sprintf("OID:%s@%s", substrateID, tenantID)
	userOID := fmt.Sprintf("OID:%s@%s", selectedMember.ID, tenantID)
	auditUser := "8:orgid:" + selectedMember.ID

	target := fabricprobe.Target{
		Team:    selectedTeam.ID,
		Channel: channelOID,
		User:    userOID,
		Role:    targetRoleNum,
	}

	fmt.Fprintf(out, "\n✓ Target de intercepción configurado:\n")
	fmt.Fprintf(out, "  Equipo: %s\n", selectedTeam.DisplayName)
	fmt.Fprintf(out, "  Canal privado: %s\n", selectedChannel.DisplayName)
	fmt.Fprintf(out, "  Miembro: %s\n", selectedMember.DisplayName)
	fmt.Fprintf(out, "  Cambio a realizar en Teams: cambiar rol de %s a %s (rol %d)\n\n", fromRole, toRole, targetRoleNum)

	return &resolvedTarget{
		target:    target,
		threadID:  selectedChannel.ID,
		auditUser: auditUser,
		teamName:  selectedTeam.DisplayName,
		chanName:  selectedChannel.DisplayName,
		userName:  selectedMember.DisplayName,
		fromRole:  fromRole,
		toRole:    toRole,
	}, nil
}
