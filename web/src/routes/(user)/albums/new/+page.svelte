<script lang="ts">
  import { goto } from '$app/navigation';
  import UserPageLayout from '$lib/components/layouts/UserPageLayout.svelte';
  import { Button, IconButton, Text } from '@immich/ui';
  import { createAlbum, AssetMediaSize } from '@immich/sdk';
  import { getAssetMediaUrl } from '$lib/utils';
  import { handleError } from '$lib/utils/handle-error';
  import { mdiArrowLeft, mdiCheckCircle, mdiCloseCircle } from '@mdi/js';
  import { t } from 'svelte-i18n';
  import type { PageData } from './$types';

  interface Props {
    data: PageData;
  }

  let { data }: Props = $props();

  let albumName = $state('');
  let description = $state('');
  let selected = $state<Set<string>>(new Set());
  let saving = $state(false);

  const toggle = (id: string) => {
    selected = new Set(selected.has(id) ? [...selected].filter((x) => x !== id) : [...selected, id]);
  };

  const save = async () => {
    if (saving) {
      return;
    }
    saving = true;
    try {
      const name = albumName.trim() || $t('unnamed_album');
      const album = await createAlbum({
        createAlbumDto: {
          albumName: name,
          description: description.trim() === '' ? undefined : description.trim(),
          assetIds: [...selected],
        },
      });
      await goto(`/albums/${album.id}`);
    } catch (error) {
      handleError(error, $t('errors.failed_to_create_album'));
      saving = false;
    }
  };
</script>

<UserPageLayout title={$t('create_album')} scrollbar={true}>
  {#snippet buttons()}
    <div class="flex items-center gap-2">
      <Button
        size="small"
        variant="ghost"
        color="secondary"
        leadingIcon={mdiArrowLeft}
        onclick={() => goto('/albums')}
      >
        <Text class="hidden md:block">{$t('back')}</Text>
      </Button>
      <Button onclick={save} disabled={saving} size="small">
        {$t('create_album')}
      </Button>
    </div>
  {/snippet}

  <div class="mx-auto flex w-full max-w-6xl flex-col gap-4 p-4 md:p-8">
    <div class="grid gap-4 md:grid-cols-2">
      <label class="flex flex-col gap-1 text-sm">
        <Text size="small" color="muted">{$t('album_name')}</Text>
        <input
          placeholder={$t('unnamed_album')}
          bind:value={albumName}
          class="immich-form-input w-full"
          maxlength="256"
        />
      </label>
      <label class="flex flex-col gap-1 text-sm">
        <Text size="small" color="muted">{$t('description')}</Text>
        <input bind:value={description} class="immich-form-input w-full" maxlength="1024" />
      </label>
    </div>

    <div class="flex items-center gap-2 text-sm">
      <Text size="small" color="muted">{$t('select_photos')}</Text>
      <span class="rounded-full bg-immich-primary/10 px-2 py-0.5 text-xs dark:bg-immich-dark-primary/20">
        {selected.size}
      </span>
      {#if selected.size > 0}
        <IconButton
          icon={mdiCloseCircle}
          color="secondary"
          variant="ghost"
          size="small"
          aria-label={$t('deselect_all')}
          onclick={() => (selected = new Set())}
        />
      {/if}
    </div>

    <div class="grid grid-cols-3 gap-1 sm:grid-cols-4 md:grid-cols-6 xl:grid-cols-8">
      {#each data.assets as asset (asset.id)}
        {@const url = getAssetMediaUrl({ id: asset.id, cacheKey: asset.thumbhash, size: AssetMediaSize.Thumbnail })}
        <button
          type="button"
          onclick={() => toggle(asset.id)}
          class="relative aspect-square overflow-hidden rounded-md transition-all {selected.has(asset.id)
            ? 'ring-4 ring-immich-primary dark:ring-immich-dark-primary'
            : 'ring-1 ring-transparent hover:ring-gray-400'}"
          title={asset.originalFileName}
        >
          <img src={url} alt={asset.originalFileName} loading="lazy" class="h-full w-full object-cover" />
          {#if selected.has(asset.id)}
            <span class="absolute right-1 top-1 text-immich-primary dark:text-immich-dark-primary">
              <svg viewBox="0 0 24 24" width="22" height="22" fill="currentColor"><path d={mdiCheckCircle} /></svg>
            </span>
          {/if}
        </button>
      {/each}
    </div>
    {#if data.assets.length === 0}
      <div class="py-16 text-center text-sm text-gray-500">{$t('no_assets_message')}</div>
    {/if}
  </div>
</UserPageLayout>
